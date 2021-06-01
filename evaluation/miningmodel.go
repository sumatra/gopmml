package evaluation

import (
	"fmt"
	"github.com/flukeish/pmml/models"
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


func (m *MiningModel) Evaluate(input DataRow, target string) (float64, error) {
	var err error

	if !m.validated {
		return 0.0, ErrNotValidated
	}

	if !m.compiled {
		return 0.0, ErrNotCompiled
	}

	input, err = HandleInput(input, m.dd, m.td, &m.model.LocalTransformations, &m.model.MiningSchema)
	if err != nil {
		return 0.0, err
	}

	totalScore := 0.0
	mdlCnt := 0.0
	for _, tree := range m.trees {
		out, err := tree.Evaluate(input)
		if err != nil {
			return 0.0, err
		}

		score, ok := out[string(m.outputField)]
		if !ok {
			return 0.0, nil
		}

		probability, ok := out["probability"]
		if !ok {
			return 0.0, nil
		}

		if score.String() == target {
			totalScore += probability.Float64()
		} else {
			totalScore += (1 - probability.Float64())
		}

		mdlCnt += 1.0
	}

	avg := totalScore / mdlCnt

	return avg, nil
}
