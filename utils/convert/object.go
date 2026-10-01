package convert

import "encoding/json"

// ConvertObject round-trips input through JSON into Target. It is how the
// gateway's YAML config types become the context's own types without the two
// packages importing each other.
func ConvertObject[Input any, Target any](input *Input) (*Target, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	var target Target
	if err := json.Unmarshal(data, &target); err != nil {
		return nil, err
	}

	return &target, nil
}

func StringToBytes(s string) []byte {
	return []byte(s)
}

func SliceInterfaceToString(slice any) []string {
	items, ok := slice.([]any)
	if !ok {
		if typed, ok := slice.([]string); ok {
			return typed
		}

		return nil
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}

	return out
}
