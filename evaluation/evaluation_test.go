package evaluation_test

import (
	"bufio"
	"encoding/xml"
	"github.com/flukeish/pmml/evaluation"
	"github.com/flukeish/pmml/models"
	"github.com/stretchr/testify/require"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluation(t *testing.T) {
	modelFiles := make(map[string]string)
	err := filepath.Walk("testdata", func(filepath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if path.Ext(filepath) == ".xml" {
			name := strings.TrimSuffix(path.Base(filepath), path.Ext(filepath))
			modelFiles[name] = filepath
		}

		return nil
	})

	require.NoError(t, err)

	for _, modelxml := range modelFiles {
		t.Run(modelxml, func(t *testing.T) {
			f, err := os.Open(modelxml)
			require.NoError(t, err)

			r := bufio.NewReader(f)

			var doc models.PMML

			err = xml.NewDecoder(r).Decode(&doc)
			require.NoError(t, err)

			require.Len(t, doc.Models, 1)

			mdl := doc.Models[0]
			emdl, err := evaluation.NewModel(&doc.DataDictionary, doc.TransformationDictionary, mdl)
			require.NoError(t, err)

			err = emdl.Verify()
			require.NoError(t, err)

			err = emdl.Compile()
			require.NoError(t, err)

			err = emdl.Validate()
			require.NoError(t, err)
		})
	}



}
