package claudeagent

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// Validate checks a structured output against the union schema: the
// subset of JSON Schema it uses (object, array, string, boolean, enum,
// maxLength, required).
func Validate(raw json.RawMessage) error {
	var schema, v any
	if err := json.Unmarshal([]byte(unionSchema), &schema); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	return check(schema.(map[string]any), v, "$")
}

func check(s map[string]any, v any, at string) error {
	switch s["type"] {
	case "object":
		o, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want an object", at)
		}
		if req, ok := s["required"].([]any); ok {
			for _, r := range req {
				if _, ok := o[r.(string)]; !ok {
					return fmt.Errorf("%s: missing %s", at, r)
				}
			}
		}
		props, _ := s["properties"].(map[string]any)
		for k, x := range o {
			ps, ok := props[k].(map[string]any)
			if !ok {
				continue // additional properties are allowed
			}
			if err := check(ps, x, at+"."+k); err != nil {
				return err
			}
		}
	case "array":
		a, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s: want an array", at)
		}
		items, _ := s["items"].(map[string]any)
		for i, x := range a {
			if err := check(items, x, fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: want a string", at)
		}
		if ml, ok := s["maxLength"].(float64); ok && utf8.RuneCountInString(str) > int(ml) {
			return fmt.Errorf("%s: %d characters, max %d", at, utf8.RuneCountInString(str), int(ml))
		}
		if enum, ok := s["enum"].([]any); ok {
			found := false
			for _, e := range enum {
				found = found || e == str
			}
			if !found {
				return fmt.Errorf("%s: %q not in %v", at, str, enum)
			}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: want a boolean", at)
		}
	}
	return nil
}
