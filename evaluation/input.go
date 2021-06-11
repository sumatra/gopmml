package evaluation

import (
	"fmt"
	"strconv"

	"github.com/flukeish/pmml/models"
)

func HandleExpression(expr models.Expression, in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (Value, error) {
	fieldRef, ok := expr.(*models.FieldRef)
	if ok {
		return in[string(fieldRef.Field)], nil
	}
	constExpr, ok := expr.(*models.Constant)
	if ok {
		if constExpr.DataType == "double" {
			v, err := strconv.ParseFloat(constExpr.Value, 64)
			if err != nil {
				return NewValue(nil), err
			}
			return NewValue(v), nil
		}
		return NewValue(nil), fmt.Errorf("unsupported data type: %s", constExpr.DataType)
	}
	apply, ok := expr.(*models.Apply)
	if ok {
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
		} else if apply.Function == "*" || apply.Function == "+" || apply.Function == "-" || apply.Function == "/" {
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
		return NewValue(nil), fmt.Errorf("unsupported apply function: %s", apply.Function)
	}
	return NewValue(nil), fmt.Errorf("unsupported transformation: %v", expr)
}

func HandleInput(in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (DataRow, error) {
	for _, df := range lt.DerivedFields {
		expr_val, err := HandleExpression(df.Expression, in, dd, td, lt, ms)
		if err != nil {
			return nil, err
		}
		in[df.Name] = expr_val
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
