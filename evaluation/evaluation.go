package evaluation

import (
	"fmt"
	"github.com/flukeish/pmml/models"
	"math"
)

type Model interface {
	Verify() error
	Compile() error
	Validate() error
	Evaluate(input DataRow) (DataRow, error)
}


func NewModel(dd *models.DataDictionary, td *models.TransformationDictionary, mdl models.ModelElement) (Model, error){
	switch v := mdl.(type) {
	case *models.TreeModel:
		return NewTreeModel(dd, td, v)
	case *models.MiningModel:
		return NewMiningModel(dd, td, v)
	case *models.RegressionModel:
		return NewRegressionModel(dd, td, v)
	default:
		return nil, fmt.Errorf("invalid model type %T", v)
	}
}

func verifyModel(m Model, mv *models.ModelVerification) error {
	if mv == nil {
		return nil
	}

	outputMap := make(map[string]string)

	for _, vField := range mv.VerificationFields.Fields {
		outputMap[vField.Column] = string(vField.Field)
	}

	for _, vRow := range mv.InlineTable.Rows {
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
					if math.Abs(expected - predicted) > 1e-6 {
						return fmt.Errorf("%s: expected %f, predicted %f", k, expected, predicted)
					}
				}
			}
		}
	}

	return nil
}