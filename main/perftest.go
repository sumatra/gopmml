package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"github.com/flukeish/pmml/evaluation"
	"github.com/flukeish/pmml/models"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"runtime/pprof"
	"strings"
	"time"
)

func main() {
	f, err := os.Create("/tmp/cpu.prof")
	if err != nil {
		panic(err)
	}
	pprof.StartCPUProfile(f)
	defer pprof.StopCPUProfile()

	modelFiles := make(map[string]string)
	err = filepath.Walk("evaluation/testdata/perf", func(filepath string, info os.FileInfo, err error) error {
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

	if err != nil {
		panic(err)
	}

	for _, modelxml := range modelFiles {
		f, err := os.Open(modelxml)
		if err != nil {
			panic(err)
		}

		r := bufio.NewReader(f)

		var doc models.PMML

		err = xml.NewDecoder(r).Decode(&doc)
		if err != nil {
			panic(err)
		}


		mdl := doc.Models[0]
		emdl, err := evaluation.NewModel(&doc.DataDictionary, &doc.TransformationDictionary, mdl)

		if err != nil {
			panic(err)
		}

		iterations := 100000
		t0 := time.Now()

		for count := 0; count < iterations; count++ {
			input := make(evaluation.DataRow)
			input["is_email_domain_free"] = evaluation.NewValue(rand.Float64())
			input["emails_per_bank"] = evaluation.NewValue(rand.Float64())
			input["dollars_out_by_email"] = evaluation.NewValue(rand.Float64())
			input["dollars_in_out_1h"] = evaluation.NewValue(rand.Float64())
			input["amount"] = evaluation.NewValue(rand.Float64())
			input["emails_per_device"] = evaluation.NewValue(rand.Float64())

			_, err := emdl.Evaluate(input)
			if err != nil {
				panic(err)
			}
		}

		t1 := time.Now()

		delay := t1.UnixNano() - t0.UnixNano()
		fmt.Printf("evalution took %f nanoseconds per call\n", float64(delay) / float64(iterations))

	}
}
