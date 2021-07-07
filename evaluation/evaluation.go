package evaluation

import (
	"errors"
	"fmt"
	"github.com/flukeish/pmml/models"
	"math"
	"strconv"
	"strings"
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
				delta := expected - predicted
				if math.Abs(delta) > 1e-5 * expected {
					return fmt.Errorf("%s: expected %f, predicted %f, error %f", k, expected, predicted, delta)
				}
			}
		}
	}

	return nil
}

func getRawValue(dt models.DataType, val string) (interface{}, error) {
	switch dt {
	case models.DataTypeBoolean:
		return strconv.ParseBool(val)
	case models.DataTypeDouble:
		return strconv.ParseFloat(val, 64)
	case models.DataTypeFloat:
		return strconv.ParseFloat(val, 64)
	case models.DataTypeInteger:
		return strconv.ParseInt(val, 10, 64)
	case models.DataTypeString:
		return val, nil
	}

	return nil, errors.New("invalid data type")
}
