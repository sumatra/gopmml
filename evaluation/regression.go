package evaluation

import (
	"fmt"
	"math"

	"github.com/flukeish/pmml/models"
)

type RegressionModel struct {
	validated bool
	compiled  bool

	dd *models.DataDictionary
	td *models.TransformationDictionary

	model *models.RegressionModel
}

func NewRegressionModel(dd *models.DataDictionary, td *models.TransformationDictionary, model *models.RegressionModel) (*RegressionModel, error) {
	m := new(RegressionModel)
	m.dd = dd
	m.td = td
	m.model = model

	return m, nil
}

func (m *RegressionModel) Compile() error {

	return nil
}

func (m *RegressionModel) Validate() error {
	if string(m.model.RegressionNormalizationMethod) == "" {
		m.model.RegressionNormalizationMethod = models.RegressionNormalizationMethodNone
	}

	if m.model.RegressionNormalizationMethod != models.RegressionNormalizationMethodNone &&
		m.model.RegressionNormalizationMethod != models.RegressionNormalizationMethodLogit &&
		m.model.RegressionNormalizationMethod != models.RegressionNormalizationMethodSoftmax &&
		m.model.RegressionNormalizationMethod != models.RegressionNormalizationMethodExp &&
		m.model.RegressionNormalizationMethod != models.RegressionNormalizationMethodCloglog &&
		m.model.RegressionNormalizationMethod != models.RegressionNormalizationMethodCauchit {
		return fmt.Errorf("unsupported regression normalization method %s", m.model.RegressionNormalizationMethod)
	}

	if m.model.FunctionName != models.MiningFunctionClassification {
		return fmt.Errorf("unsupported mining function %s", m.model.FunctionName)
	}

	for _, output := range m.model.Output.Fields {
		if output.Feature != models.ResultFeatureProbability {
			return fmt.Errorf("unsupported output feature %s", output.Feature)
		}
	}

	return nil
}

func (m *RegressionModel) Verify() error {
	return verifyModel(m, m.model.ModelVerification)
}

func (m *RegressionModel) normalize(term float64) (float64, error) {
	switch m.model.RegressionNormalizationMethod {
	case models.RegressionNormalizationMethodSoftmax:
		return 1.0 / (1.0 + math.Exp(-term)), nil
	case models.RegressionNormalizationMethodLogit:
		return 1.0 / (1.0 + math.Exp(-term)), nil
	case models.RegressionNormalizationMethodExp:
		return math.Exp(term), nil
	case models.RegressionNormalizationMethodCloglog:
		return 1.0 - math.Exp(-math.Exp(term)), nil
	case models.RegressionNormalizationMethodCauchit:
		return 0.5 + (1/math.Pi)*math.Atan(term), nil
	}
	return 0.0, fmt.Errorf("unrecognized normalization method: %v", m.model.RegressionNormalizationMethod)
}

func (m *RegressionModel) Evaluate(input DataRow) (DataRow, error) {
	if m.model.FunctionName == "regression" {
		return m.EvaluateRegression(input)
	} else if m.model.FunctionName == "classification" && len(m.model.Output.Fields) == 2 {
		return m.EvaluateBinaryClassification(input)
	} else if m.model.FunctionName == "classification" {
		return m.EvaluateMulticlassClassification(input)
	}
	return nil, fmt.Errorf("unrecognized regression function name: %s", m.model.FunctionName)
}

func (m *RegressionModel) EvaluateRegression(input DataRow) (DataRow, error) {
	out := make(DataRow)

	if len(m.model.RegressionTables) != 1 {
		return out, fmt.Errorf("regression expects exactly 1 RegressionTable, found: %d", len(m.model.RegressionTables))
	}
	rtbl := m.model.RegressionTables[0]

	if len(m.model.Output.Fields) != 1 {
		return out, fmt.Errorf("regression expects exactly 1 OutputField, found: %d", len(m.model.Output.Fields))
	}
	outField := m.model.Output.Fields[0]

	y := rtbl.Intercept
	for _, np := range rtbl.NumericPredictor {
		term, ok := input[np.Name]
		if !ok {
			return nil, fmt.Errorf("regression missing term %s", np.Name)
		}
		indVar := term.Float64()
		coeff := np.Coefficient
		y += coeff * indVar
	}

	pred, err := m.normalize(y)
	if err != nil {
		return out, err
	}
	out[string(outField.Name)] = NewValue(pred)

	return out, nil
}

func (m *RegressionModel) EvaluateBinaryClassification(input DataRow) (DataRow, error) {
	out := make(DataRow)

	finalPred := 1.0

	for _, rtbl := range m.model.RegressionTables {
		pred := rtbl.Intercept

		if len(rtbl.NumericPredictor) > 0 {
			for _, np := range rtbl.NumericPredictor {
				term, ok := input[np.Name]
				if !ok {
					return nil, fmt.Errorf("regression missing term %s", np.Name)
				}
				normalized, err := m.normalize(term.Float64())
				if err != nil {
					return out, err
				}
				pred += normalized
			}
		} else {
			pred = finalPred
		}

		finalPred = finalPred - pred

		for _, output := range m.model.Output.Fields {
			if rtbl.TargetCategory == output.Value {
				out[string(output.Name)] = NewValue(pred)
			}
		}
	}

	return out, nil
}

func (m *RegressionModel) EvaluateMulticlassClassification(input DataRow) (DataRow, error) {
	out := make(DataRow)

	outFields := make(map[string]string)
	for _, output := range m.model.Output.Fields {
		outFields[output.Value] = string(output.Name)
	}

	y := make([]float64, len(m.model.RegressionTables))
	targets := make([]string, len(m.model.RegressionTables))
	for i, rtbl := range m.model.RegressionTables {
		targets[i] = outFields[rtbl.TargetCategory]
		y[i] = rtbl.Intercept

		for _, np := range rtbl.NumericPredictor {
			term, ok := input[np.Name]
			if !ok {
				return nil, fmt.Errorf("regression missing term %s", np.Name)
			}
			indVar := term.Float64()
			coeff := np.Coefficient
			y[i] += coeff * indVar
		}
	}

	for i, target := range targets {
		switch m.model.RegressionNormalizationMethod {
		case models.RegressionNormalizationMethodSoftmax:
			s := 0.0
			for _, yVal := range y {
				s += math.Exp(yVal)
			}
			out[target] = NewValue(math.Exp(y[i]) / s)
		case models.RegressionNormalizationMethodSimplemax:
			s := 0.0
			for _, yVal := range y {
				s += yVal
			}
			out[target] = NewValue(y[i] / s)
		default:
			return out, fmt.Errorf("unsupported normalization method: %v", m.model.RegressionNormalizationMethod)
		}
	}
	return out, nil
}
