package evaluation

import (
	"fmt"
	"github.com/flukeish/pmml/models"
	"math"
)

type MiningModel struct {
	validated bool
	compiled  bool

	dd    *models.DataDictionary
	td    *models.TransformationDictionary
	model *models.MiningModel

	trees []*TreeModel

	fieldTypes  map[models.FieldName]models.DataType
	outputField models.FieldName
	outputFields []models.OutputField
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
	outputMap := make(map[string]string)

	for _, vField := range m.model.ModelVerification.VerificationFields.Fields {
		outputMap[vField.Column] = string(vField.Field)
	}

	for _, vRow := range m.model.ModelVerification.InlineTable.Rows {
		input := make(DataRow)
		for k, v := range vRow {
			input[k] = NewValue(v)
		}

		output, err := m.Evaluate(input)
		if err != nil {
			return err
		}

		for k, v := range input {
			outKey := fmt.Sprintf("data:%s", k)
			resKey, ok := outputMap[outKey]
			if ok {
				expected := v.Float64()
				predictedVal, ok := output[resKey]
				predicted := predictedVal.Float64()
				if ok {
					if math.Abs(expected - predicted) > 1e6 {
						return fmt.Errorf("%s: expected %f, predicted %f", k, expected, predicted)
					}
				}
			}
		}

		for k, v := range output {
			col, ok := outputMap[k]
			if ok {
				expected := input[col].Float64()
				predicted := v.Float64()
				if expected != predicted {
					return fmt.Errorf("%s: expected %f, predicted %f", col, expected, predicted)
				}
			}
		}
	}
	return nil
}

func (m *MiningModel) Validate() error {
	for _, f := range m.model.Output.Fields {
		if f.Feature != models.ResultFeatureProbability {
			return fmt.Errorf("unsupported output feature type %s", f.Feature)
		}
	}

	m.validated = true
	return nil
}

func (m *MiningModel) Compile() error {
	m.outputFields = m.model.Output.Fields

	for _, f := range m.model.MiningSchema.MiningFields {
		if f.UsageType == models.FieldUsageTypeTarget {
			m.outputField = f.Name
		}
	}

	m.fieldTypes = make(map[models.FieldName]models.DataType)

	for _, df := range m.dd.DataFields {
		m.fieldTypes[df.Name] = df.DataType
	}

	for _, tf := range m.model.LocalTransformations.DerivedFields {
		m.fieldTypes[models.FieldName(tf.Name)] = tf.DataType
	}

	if m.model.Segmentation.MultipleModelMethod != models.MultipleModelMethodAverage {
		return fmt.Errorf("unsupported multiple model method %s", m.model.Segmentation.MultipleModelMethod)
	}


	for _, seg := range m.model.Segmentation.Segments {
		if len(seg.Models) != 1 {
			return fmt.Errorf("unsupported single segment model group size %d", len(seg.Models))
		}

		tmdl, ok := seg.Models[0].(*models.TreeModel)
		if !ok {
			return fmt.Errorf("unsupported model type %T", seg.Models[0])
		}

		mdl, err := NewTreeModel(m.dd, m.td, tmdl)
		if err != nil {
			return err
		}

		mdl.outputField = m.outputField
		for _, tf := range m.model.LocalTransformations.DerivedFields {
			mdl.fieldTypes[models.FieldName(tf.Name)] = tf.DataType
		}

		m.trees = append(m.trees, mdl)
	}

	m.compiled = true

	return nil
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

	mdlCnt := 0.0


	labelScores := make([]float64, 2, 2)
	labels := make([]string, 2, 2)

	labels[0] = string(m.outputFields[0].Name)
	labels[1] = string(m.outputFields[1].Name)

	for _, tree := range m.trees {
		out, err := tree.Evaluate(input)
		if err != nil {
			return nil, err
		}

		score, ok := out[string(m.outputField)]
		if !ok {
			return nil, nil
		}

		confidence, ok := out["confidence"]
		if !ok {
			return nil, nil
		}

		if labels[0] == fmt.Sprintf("probability(%s)", score.String()) {
			labelScores[0] += confidence.Float64()
			labelScores[1] += 1 - confidence.Float64()
		} else {
			labelScores[1] += confidence.Float64()
			labelScores[0] += 1 - confidence.Float64()
		}

		mdlCnt += 1.0
	}

	out := make(DataRow)
	out[labels[0]] = NewValue(labelScores[0] / mdlCnt)
	out[labels[1]] = NewValue(labelScores[1] / mdlCnt)

	return out, nil
}
