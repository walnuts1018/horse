package main

import (
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/walnuts1018/horse/internal/evaluation"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("horse-evaluate", flag.ContinueOnError)
	datasetPath := flags.String("dataset", "", "path to versioned offline evaluation JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *datasetPath == "" || flags.NArg() != 0 {
		return errors.New("--dataset is required and positional arguments are not accepted")
	}
	file, err := os.Open(*datasetPath)
	if err != nil {
		return fmt.Errorf("open evaluation dataset: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, file.Close()) }()
	var dataset evaluation.Dataset
	if err := json.UnmarshalRead(io.LimitReader(file, 32<<20), &dataset); err != nil {
		return fmt.Errorf("decode evaluation dataset: %w", err)
	}
	if dataset.Version == "" || len(dataset.Cases) == 0 {
		return errors.New("evaluation dataset requires a version and at least one case")
	}
	seenIDs := make(map[string]struct{}, len(dataset.Cases))
	for _, testCase := range dataset.Cases {
		if testCase.ID == "" {
			return errors.New("evaluation case ID is required")
		}
		if _, exists := seenIDs[testCase.ID]; exists {
			return fmt.Errorf("duplicate evaluation case ID %q", testCase.ID)
		}
		seenIDs[testCase.ID] = struct{}{}
	}
	return json.MarshalWrite(output, evaluation.Score(dataset))
}
