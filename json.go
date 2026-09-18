package typesafe

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// The API tags a question and an answer with a type field that names its kind.
// Go has no tagged unions, so this file holds the encoding both directions:
// marshalTagged writes the field beside a struct's own fields, and kindOf and
// unmarshalTagged read it back.

// marshalTagged encodes value and inserts the type discriminator, which the
// question and answer structs do not carry as a field.
func marshalTagged(kind string, value any) ([]byte, error) {
	fields, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	tagged := []byte(`{"type":` + strconv.Quote(kind))
	if len(fields) == len("{}") {
		return append(tagged, '}'), nil
	}
	// {"type":"noul" + , + "noul":0.5} -> {"type":"noul","noul":0.5}
	return slices.Concat(tagged, []byte{','}, fields[1:]), nil
}

// kindOf reads the type discriminator of a question or an answer.
func kindOf(raw json.RawMessage) (string, error) {
	var tagged struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &tagged); err != nil {
		return "", fmt.Errorf("type: %w", err)
	}
	return tagged.Type, nil
}

// unmarshalTagged decodes raw into T, whose own fields carry no discriminator.
func unmarshalTagged[T any](raw json.RawMessage) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}

// sortedKeys returns the keys of m in sorted order, so a failure names the
// same entry on every run.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
