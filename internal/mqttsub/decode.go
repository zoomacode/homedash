package mqttsub

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// decodeValue extracts a display string from a payload.
//
// When field is empty: a bare number, a JSON object with a "value" key, or
// arbitrary text are returned as-is.
//
// When field is non-empty: payload must parse as a JSON object; the dotted
// path (e.g. "temperature" or "particles.p25um") is walked and the leaf
// scalar is returned as a string. Missing keys are an error.
func decodeValue(b []byte, field string) (string, error) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", nil
	}

	if field != "" {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err != nil {
			return "", fmt.Errorf("payload is not JSON object: %w", err)
		}
		v, err := walk(obj, strings.Split(field, "."))
		if err != nil {
			return "", err
		}
		return scalarToString(v)
	}

	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return s, nil
	}
	if strings.HasPrefix(s, "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(s), &obj); err == nil {
			if v, ok := obj["value"]; ok {
				return fmt.Sprint(v), nil
			}
		}
	}
	return s, nil
}

func walk(obj map[string]any, parts []string) (any, error) {
	cur := any(obj)
	for i, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field %q: not an object at %q", strings.Join(parts, "."), strings.Join(parts[:i], "."))
		}
		v, ok := m[p]
		if !ok {
			return nil, fmt.Errorf("field %q: missing key %q", strings.Join(parts, "."), p)
		}
		cur = v
	}
	return cur, nil
}

func scalarToString(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		return x.String(), nil
	default:
		return "", fmt.Errorf("field value is not a scalar: %T", v)
	}
}
