package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const maxExperimentConfigBytes = 1 << 20

func readJSONConfig(path string, out any) error {
	var reader io.Reader
	if path == "-" {
		reader = os.Stdin
	} else {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxExperimentConfigBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxExperimentConfigBytes {
		return fmt.Errorf("config exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid config JSON: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("config must contain one JSON object")
	}
	return nil
}
