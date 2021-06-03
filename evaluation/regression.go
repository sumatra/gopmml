package evaluation

import (
	"fmt"
	"github.com/flukeish/pmml/models"
	"math"
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

func (m *RegressionModel) normalize(term float64) float64 {
	switch m.model.RegressionNormalizationMethod {
	case models.RegressionNormalizationMethodSoftmax:
		return 1.0 / (1.0 + math.Exp(-term))
	case models.RegressionNormalizationMethodLogit:
		return 1.0 / (1.0 + math.Exp(-term))
	case models.RegressionNormalizationMethodExp:
		return math.Exp(term)
	case models.RegressionNormalizationMethodCloglog:
		return 1.0 - math.Exp(-math.Exp(term))
	case models.RegressionNormalizationMethodCauchit:
		return 0.5 + (1 / math.Pi) * math.Atan(term)
	default:
		return term
	}
}

func (m *RegressionModel) Evaluate(input DataRow) (DataRow, error) {
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

				if m.model.RegressionNormalizationMethod == models.RegressionNormalizationMethodLogit {
					pred += m.normalize(term.Float64())
				}
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
