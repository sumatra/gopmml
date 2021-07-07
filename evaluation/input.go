package evaluation

import (
	"fmt"
	"github.com/pkg/errors"
	"strconv"

	"github.com/flukeish/pmml/models"
)

func handleConstantExpression(constExpr *models.Constant) (Value, error) {
	if constExpr.DataType == models.DataTypeDouble {
		v, err := strconv.ParseFloat(constExpr.Value, 64)
		if err != nil {
			return NewValue(nil), err
		}
		return NewValue(v), nil
	}

	return NewValue(nil), fmt.Errorf("unsupported data type: %s", constExpr.DataType)
}

func handleApplyExpression(apply *models.Apply, in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (Value, error) {
	if apply.Function == "sum" {
		s := 0.0
		for _, child := range apply.Expressions {
			child_val, err := HandleExpression(child, in, dd, td, lt, ms)
			if err != nil {
				return child_val, err
			}
			s += child_val.Float64()
		}
		return NewValue(s), nil
	}

	if apply.Function == "*" || apply.Function == "+" || apply.Function == "-" || apply.Function == "/" {
		if len(apply.Expressions) != 2 {
			return NewValue(nil), fmt.Errorf("function %s expects 2 arguments, found %d", apply.Function, len(apply.Expressions))
		}
		val1, err := HandleExpression(apply.Expressions[0], in, dd, td, lt, ms)
		if err != nil {
			return NewValue(nil), err
		}
		val2, err := HandleExpression(apply.Expressions[1], in, dd, td, lt, ms)
		if err != nil {
			return NewValue(nil), err
		}
		if apply.Function == "*" {
			return NewValue(val1.Float64() * val2.Float64()), nil
		}
		if apply.Function == "+" {
			return NewValue(val1.Float64() + val2.Float64()), nil
		}
		if apply.Function == "-" {
			return NewValue(val1.Float64() - val2.Float64()), nil
		}
		if apply.Function == "/" {
			return NewValue(val1.Float64() / val2.Float64()), nil
		}
	}

	if apply.Function == "isMissing" {
		if len(apply.Expressions) != 1 {
			return NewValue(nil), fmt.Errorf("function %s expects 1 argument, found %d", apply.Function, len(apply.Expressions))
		}

		missing, err := isMissing(apply, in, dd, td, lt, ms)
		if err != nil {
			return NewValue(false), err
		}

		return NewValue(missing), nil
	}

	if apply.Function == "isNotMissing" {
		if len(apply.Expressions) != 1 {
			return NewValue(nil), fmt.Errorf("function %s expects 1 argument, found %d", apply.Function, len(apply.Expressions))
		}

		missing, err := isMissing(apply, in, dd, td, lt, ms)
		if err != nil {
			return NewValue(false), err
		}

		return NewValue(!missing), nil
	}

	if apply.Function == "if" {
		if len(apply.Expressions) != 3 {
			return NewValue(nil), fmt.Errorf("function %s expects 1 argument, found %d", apply.Function, len(apply.Expressions))
		}

		condition, err := HandleExpression(apply.Expressions[0], in, dd, td, lt, ms)
		if err != nil {
			return NewValue(nil), err
		}

		if condition.Bool() {
			return HandleExpression(apply.Expressions[1], in, dd, td, lt, ms)
		} else {
			return HandleExpression(apply.Expressions[2], in, dd, td, lt, ms)
		}

	}

	return NewValue(nil), fmt.Errorf("unsupported apply function: %s", apply.Function)
}

func isMissing(apply *models.Apply, in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (bool, error) {
	if len(apply.Expressions) != 1 {
		return true, fmt.Errorf("function %s expects 1 argument, found %d", apply.Function, len(apply.Expressions))
	}

	out, err := HandleExpression(apply.Expressions[0], in, dd, td, lt, ms)
	if err != nil {
		return true, err
	}

	if out.Raw() == nil {
		return true, nil
	}

	return false, nil
}

func handleMapValuesExpression(expr *models.MapValues, in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (Value, error) {
	columns := make(map[string]Value)

	for _, pair := range expr.FieldColumnPairs {
		data, ok := in[pair.Field]
		if ok {
			columns[pair.Column] = data
		}
	}

	for _, mapping := range expr.InlineTables {
		for _, row := range mapping.Rows {
			if row["input"] == columns["data:input"].String() {
				columns["data:output"] = NewValue(row["output"])
			}
		}
	}

	output, ok := columns["data:output"]
	if ok {
		return output, nil
	}

	if expr.DefaultValue != nil {
		defaultValue, err := getRawValue(expr.DataType, *expr.DefaultValue)
		if err != nil {
			return NewValue(nil), err
		}

		return NewValue(defaultValue), nil
	}

	input, ok := columns["data:input"]
	if ok {
		return input, nil
	}

	return NewValue(nil), errors.New("unexpected expression")
}

func HandleExpression(expr models.Expression, in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (Value, error) {
	fieldRef, ok := expr.(*models.FieldRef)
	if ok {
		return in[string(fieldRef.Field)], nil
	}
	constExpr, ok := expr.(*models.Constant)
	if ok {
		return handleConstantExpression(constExpr)
	}
	apply, ok := expr.(*models.Apply)
	if ok {
		return handleApplyExpression(apply, in, dd, td, lt, ms)
	}
	mapExpr, ok := expr.(*models.MapValues)
	if ok {
		return handleMapValuesExpression(mapExpr, in, dd, td, lt, ms)
	}
	return NewValue(nil), fmt.Errorf("unsupported transformation: %T", expr)
}

func HandleInput(in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (DataRow, error) {
	for _, df := range lt.DerivedFields {
		expr_val, err := HandleExpression(df.Expression, in, dd, td, lt, ms)
		if err != nil {
			return nil, err
		}

		if df.OpType == models.OpTypeContinuous {
			in[df.Name] = NewValue(expr_val.Float64())
		} else {
			in[df.Name] = expr_val
		}
	}

	err := validate(in, ms)
	if err != nil {
		return nil, err
	}

	return in, nil
}

func validate(in DataRow, ms *models.MiningSchema) error {
	if ms == nil {
		return nil
	}

	return nil
}
