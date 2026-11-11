package hostlib

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

func marshalIndented(v any) ([]byte, error) {
	data, err := json.Marshal(v, jsontext.WithIndent("  "))
	if err != nil {
		return nil, fmt.Errorf("encoding hosts: %w", err)
	}
	return append(data, '\n'), nil
}
