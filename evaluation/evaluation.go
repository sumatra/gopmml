package evaluation

import (
	"fmt"
	"math"
	"strings"

	"github.com/flukeish/pmml/models"
)

type Model interface {
	Verify() error
	Compile() error
	Validate() error
	Evaluate(input DataRow) (DataRow, error)
}

func NewModel(dd *models.DataDictionary, td *models.TransformationDictionary, mdl models.ModelElement) (Model, error) {
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
	inputMap := make(map[string]string)

	for _, vField := range mv.VerificationFields.Fields {
		if !strings.HasPrefix(vField.Column, "data:") {
			return fmt.Errorf("verification field missing 'data:' prefix: %s", vField.Column)
		}
		colName := vField.Column[5:]
		if vField.Precision == nil {
			inputMap[colName] = string(vField.Field)
		} else {
			outputMap[colName] = string(vField.Field)
		}
	}

	for _, vRow := range mv.InlineTable.Rows {
		input := make(DataRow)
		for k, v := range vRow {
			_, isInput := inputMap[k]
			if isInput {
				input[k] = NewValue(v)
			}
		}

		output, err := m.Evaluate(input)
		if err != nil {
			return err
		}

		for k, v := range vRow {
			fieldName, isOutput := outputMap[k]
			if isOutput {
				expected := NewValue(v).Float64()
				predictedVal, ok := output[fieldName]
				if !ok {
					return fmt.Errorf("expected output: %s not produced by model", fieldName)
				}
				predicted := predictedVal.Float64()
				if math.Abs(expected-predicted) > 1e-6 {
					return fmt.Errorf("%s: expected %f, predicted %f", k, expected, predicted)
				}
			}
		}
	}

	return nil
}
