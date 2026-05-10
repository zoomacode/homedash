package mqttsub

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// decodeValue extracts a single display string from a payload that may be
// a bare number, a JSON object with a "value" key, or arbitrary text.
func decodeValue(b []byte) (string, error) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", nil
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
