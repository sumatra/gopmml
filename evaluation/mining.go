package evaluation

import (
	"errors"
	"fmt"
	"github.com/flukeish/pmml/models"
)

type MiningModel struct {
	validated bool
	compiled  bool

	dd    *models.DataDictionary
	td    *models.TransformationDictionary
	model *models.MiningModel

	segmodels []Model

	fieldTypes   map[models.FieldName]models.DataType
	targetField  models.FieldName
	outputFields []models.OutputField
	targets      []models.Target
}

func NewMiningModel(dd *models.DataDictionary, td *models.TransformationDictionary, model *models.MiningModel) (*MiningModel, error) {
	m := new(MiningModel)
	m.dd = dd
	m.td = td
	m.model = model

	err := m.Validate()
	if err != nil {
		return nil, err
	}

	err = m.Compile()
	if err != nil {
		return nil, err
	}

	return m, nil
}

func (m *MiningModel) Verify() error {
	return verifyModel(m, m.model.ModelVerification)
}

func (m *MiningModel) Validate() error {
	if string(m.model.Segmentation.MultipleModelMethod) == "" {
		m.model.Segmentation.MultipleModelMethod = models.MultipleModelMethodSum
	}
	if m.model.Segmentation.MultipleModelMethod != models.MultipleModelMethodAverage &&
		m.model.Segmentation.MultipleModelMethod != models.MultipleModelMethodModelChain &&
		m.model.Segmentation.MultipleModelMethod != models.MultipleModelMethodSum &&
		m.model.Segmentation.MultipleModelMethod != models.MultipleModelMethodSelectFirst {
		return fmt.Errorf("unsupported multiple model method %s", m.model.Segmentation.MultipleModelMethod)
	}

	for _, f := range m.model.Output.Fields {
		if f.Feature != "" && f.Feature != models.ResultFeatureProbability {
			return fmt.Errorf("unsupported output feature type %s", f.Feature)
		}
	}

	m.validated = true
	return nil
}

func (m *MiningModel) Compile() error {
	m.outputFields = m.model.Output.Fields
	m.targets = m.model.Targets.Targets

	for _, f := range m.model.MiningSchema.MiningFields {
		if f.UsageType == models.FieldUsageTypeTarget {
			m.targetField = f.Name
		}
	}

	m.fieldTypes = make(map[models.FieldName]models.DataType)

	for _, df := range m.dd.DataFields {
		m.fieldTypes[df.Name] = df.DataType
	}

	for _, tf := range m.model.LocalTransformations.DerivedFields {
		m.td.DerivedFields = append(m.td.DerivedFields, tf)
	}

	for _, df := range m.td.DerivedFields {
		m.fieldTypes[models.FieldName(df.Name)] = df.DataType
	}

	for _, seg := range m.model.Segmentation.Segments {
		if len(seg.Models) != 1 {
			return fmt.Errorf("unsupported single segment model group size %d", len(seg.Models))
		}

		segmdl := seg.Models[0]
		mdl, err := NewModel(m.dd, m.td, segmdl)

		if err != nil {
			return err
		}

		m.segmodels = append(m.segmodels, mdl)
	}

	m.compiled = true

	return nil
}

func (m *MiningModel) evaluateMultipleModelSelectFirst(input DataRow) (DataRow, error) {
	for _, segModel := range m.segmodels {
		segOut, err := segModel.Evaluate(input)
		if err != nil {
			return nil, err
		}

		return segOut, nil
	}

	return nil, errors.New("no segment models to evaluate")
}

func (m *MiningModel) evaluateMultipleModelChain(input DataRow) (DataRow, error) {
	lastOut := make(DataRow)

	for _, segModel := range m.segmodels {
		for k, v := range lastOut {
			input[k] = v
		}

		segOut, err := segModel.Evaluate(input)
		if err != nil {
			return nil, err
		}

		lastOut = segOut
	}

	return lastOut, nil
}

func (m *MiningModel) evaluateMultipleModelSum(input DataRow) (DataRow, error) {
	scoreMap := make(map[models.FieldName]float64)

	for _, field := range m.outputFields {
		scoreMap[field.Name] = 0.0
	}

	for _, target := range m.targets {
		scoreMap[target.Field] = 0.0
	}

	for _, segmodel := range m.segmodels {
		segout, err := segmodel.Evaluate(input)
		if err != nil {
			return nil, err
		}

		for k, v := range scoreMap {
			if m.model.FunctionName == models.MiningFunctionClassification {
				outval, ok := segout[string(k)]
				if ok {
					scoreMap[k] = v + outval.Float64()
				}
			} else {
				outval, ok := segout["score"]
				if ok {
					scoreMap[k] = v + outval.Float64()
				}
			}
		}
	}

	for _, target := range m.targets {
		if target.RescaleFactor != nil {
			scoreMap[target.Field] = scoreMap[target.Field] * *target.RescaleFactor
		}
		if target.RescaleConstant != nil {
			scoreMap[target.Field] = scoreMap[target.Field] + *target.RescaleConstant
		}
	}


	out := make(DataRow)
	for k, v := range scoreMap {
		out[string(k)] = NewValue(v)
	}

	return out, nil
}

func (m *MiningModel) evaluateMultipleModelAverage(input DataRow) (DataRow, error) {
	out, err := m.evaluateMultipleModelSum(input)
	if err != nil {
		return nil, err
	}

	for lbl, score := range out {
		out[lbl] = NewValue(score.Float64() / float64(len(m.model.Segmentation.Segments)))
	}

	return out, nil
}

func (m *MiningModel) Evaluate(input DataRow) (DataRow, error) {
	var err error

	if !m.validated {
		return nil, ErrNotValidated
	}

	if !m.compiled {
		return nil, ErrNotCompiled
	}

	input, err = HandleInput(input, m.dd, m.td, &m.model.LocalTransformations, &m.model.MiningSchema)
	if err != nil {
		return nil, err
	}

	switch m.model.Segmentation.MultipleModelMethod {
	case models.MultipleModelMethodAverage:
		return m.evaluateMultipleModelAverage(input)
	case models.MultipleModelMethodSum:
		return m.evaluateMultipleModelSum(input)
	case models.MultipleModelMethodModelChain:
		return m.evaluateMultipleModelChain(input)
	case models.MultipleModelMethodSelectFirst:
		return m.evaluateMultipleModelSelectFirst(input)
	default:
		return nil, fmt.Errorf("unsupported multiple model method %s", m.model.Segmentation.MultipleModelMethod)
	}
}
