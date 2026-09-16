package schema

import (
	"fmt"
	"strings"
)

// Validate checks value (as produced by encoding/json.Unmarshal into
// interface{} — map[string]interface{}, []interface{}, float64, string,
// bool, or nil) against s, returning one message per violation found. An
// empty result means value conforms. This is intentionally a small,
// dependency-free subset of JSON Schema covering exactly what the schema
// Builder emits (type, properties, required, items, enum, nullable, and
// "$ref"/"$defs") — not a general-purpose validator.
func Validate(s *JSONSchema, value interface{}) []string {
	return validateAt(s, value, "$", s.Defs)
}

func validateAt(s *JSONSchema, value interface{}, path string, defs map[string]*JSONSchema) []string {
	if s == nil {
		return nil
	}
	if value == nil {
		if s.Nullable || (s.Type == "" && s.Ref == "") {
			return nil
		}
		return []string{fmt.Sprintf("%s: null is not allowed", path)}
	}
	if s.Ref != "" {
		name := strings.TrimPrefix(s.Ref, "#/$defs/")
		target, ok := defs[name]
		if !ok {
			return []string{fmt.Sprintf("%s: unresolved %s", path, s.Ref)}
		}
		return validateAt(target, value, path, defs)
	}

	switch s.Type {
	case "", "any":
		return nil
	case "string":
		str, ok := value.(string)
		if !ok {
			return []string{fmt.Sprintf("%s: expected string, got %T", path, value)}
		}
		if len(s.Enum) > 0 && !contains(s.Enum, str) {
			return []string{fmt.Sprintf("%s: %q is not one of %v", path, str, s.Enum)}
		}
		return nil
	case "integer":
		n, ok := asFloat(value)
		if !ok {
			return []string{fmt.Sprintf("%s: expected integer, got %T", path, value)}
		}
		if n != float64(int64(n)) {
			return []string{fmt.Sprintf("%s: expected integer, got fractional number %v", path, n)}
		}
		return nil
	case "number":
		if _, ok := asFloat(value); !ok {
			return []string{fmt.Sprintf("%s: expected number, got %T", path, value)}
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return []string{fmt.Sprintf("%s: expected boolean, got %T", path, value)}
		}
		return nil
	case "array":
		arr, ok := value.([]interface{})
		if !ok {
			return []string{fmt.Sprintf("%s: expected array, got %T", path, value)}
		}
		var errs []string
		for i, item := range arr {
			errs = append(errs, validateAt(s.Items, item, fmt.Sprintf("%s[%d]", path, i), defs)...)
		}
		return errs
	case "object":
		obj, ok := value.(map[string]interface{})
		if !ok {
			return []string{fmt.Sprintf("%s: expected object, got %T", path, value)}
		}
		var errs []string
		for _, req := range s.Required {
			if _, present := obj[req]; !present {
				errs = append(errs, fmt.Sprintf("%s: missing required field %q", path, req))
			}
		}
		for k, v := range obj {
			prop, ok := s.Properties[k]
			if !ok {
				continue // unknown fields are tolerated, not rejected
			}
			errs = append(errs, validateAt(prop, v, path+"."+k, defs)...)
		}
		return errs
	default:
		return nil
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func asFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
