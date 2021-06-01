package evaluation

import (
	"errors"
	"github.com/flukeish/pmml/models"
)

func HandleInput(in DataRow, dd *models.DataDictionary, td *models.TransformationDictionary, lt *models.LocalTransformations, ms *models.MiningSchema) (DataRow, error) {
	for _, df := range lt.DerivedFields {
		fieldRef, ok := df.Expression.(*models.FieldRef)
		if !ok {
			return nil, errors.New("unsupported transformation")
		}

		in[df.Name] = in[string(fieldRef.Field)]
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
