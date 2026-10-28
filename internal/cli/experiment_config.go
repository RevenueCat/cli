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
	return readJSONConfigWithLimit(path, out, maxExperimentConfigBytes)
}

func readJSONConfigWithLimit(path string, out any, maxBytes int64) error {
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
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > maxBytes {
		return fmt.Errorf("config exceeds %d MiB", maxBytes>>20)
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

func compactJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}
