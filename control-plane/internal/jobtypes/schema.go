package jobtypes

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
)

// A small JSON Schema validator: the subset job types need, with no dependency. Schemas that
// use anything outside it are refused when the registry loads (never silently ignored), so a
// type can not promise a check this code does not make.
//
// Supported: type (one name or a list), enum, const, properties, required,
// additionalProperties (true, false or a schema), items (one schema), minItems, maxItems,
// minLength, maxLength, pattern, minimum, maximum. Annotations ($schema, $id, title,
// description, default, examples, $comment) are accepted and ignored.

var supported = map[string]bool{
	"type": true, "enum": true, "const": true, "properties": true, "required": true,
	"additionalProperties": true, "items": true, "minItems": true, "maxItems": true,
	"minLength": true, "maxLength": true, "pattern": true, "minimum": true, "maximum": true,
	"$schema": true, "$id": true, "title": true, "description": true, "default": true,
	"examples": true, "$comment": true,
}

var typeNames = map[string]bool{
	"object": true, "array": true, "string": true, "number": true, "integer": true, "boolean": true, "null": true,
}

// CheckSchema reports why a schema is outside the supported subset (nil: usable).
func CheckSchema(schema map[string]interface{}) error { return checkSchema(schema, "$") }

func checkSchema(s map[string]interface{}, path string) error {
	keys := make([]string, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !supported[k] {
			return fmt.Errorf("%s: keyword %q is not supported", path, k)
		}
	}
	if t, ok := s["type"]; ok {
		for _, name := range typeList(t) {
			if !typeNames[name] {
				return fmt.Errorf("%s: unknown type %q", path, name)
			}
		}
		if len(typeList(t)) == 0 {
			return fmt.Errorf("%s: type must be a name or a list of names", path)
		}
	}
	if p, ok := s["pattern"]; ok {
		str, isStr := p.(string)
		if !isStr {
			return fmt.Errorf("%s: pattern must be a string", path)
		}
		if _, err := regexp.Compile(str); err != nil {
			return fmt.Errorf("%s: bad pattern: %v", path, err)
		}
	}
	if r, ok := s["required"]; ok {
		list, isList := r.([]interface{})
		if !isList {
			return fmt.Errorf("%s: required must be a list", path)
		}
		for _, v := range list {
			if _, isStr := v.(string); !isStr {
				return fmt.Errorf("%s: required must list property names", path)
			}
		}
	}
	if props, ok := s["properties"]; ok {
		m, isMap := props.(map[string]interface{})
		if !isMap {
			return fmt.Errorf("%s: properties must be an object", path)
		}
		for name, sub := range m {
			subMap, isSub := sub.(map[string]interface{})
			if !isSub {
				return fmt.Errorf("%s.%s: a schema must be an object", path, name)
			}
			if err := checkSchema(subMap, path+"."+name); err != nil {
				return err
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		sub, ok := s[key]
		if !ok {
			continue
		}
		switch v := sub.(type) {
		case bool:
			if key == "items" {
				return fmt.Errorf("%s: items must be a schema", path)
			}
		case map[string]interface{}:
			if err := checkSchema(v, path+"."+key); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%s: %s must be a schema", path, key)
		}
	}
	return nil
}

func typeList(t interface{}) []string {
	switch v := t.(type) {
	case string:
		return []string{v}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, ok := x.(string)
			if !ok {
				return nil
			}
			out = append(out, s)
		}
		return out
	}
	return nil
}

// Validate returns every way value (decoded JSON) breaks the schema; empty: valid.
func Validate(schema map[string]interface{}, value interface{}) []string {
	var problems []string
	validate(schema, value, "$", &problems)
	return problems
}

func validate(s map[string]interface{}, v interface{}, path string, out *[]string) {
	bad := func(format string, args ...interface{}) { *out = append(*out, path+": "+fmt.Sprintf(format, args...)) }

	if t, ok := s["type"]; ok {
		matched := false
		names := typeList(t)
		for _, name := range names {
			if isType(name, v) {
				matched = true
				break
			}
		}
		if !matched {
			bad("must be %s, not %s", strings.Join(names, " or "), kind(v))
			return // the checks below assume the right type
		}
	}
	if e, ok := s["enum"].([]interface{}); ok {
		found := false
		for _, x := range e {
			if equal(x, v) {
				found = true
				break
			}
		}
		if !found {
			bad("must be one of %s", compact(e))
		}
	}
	if c, ok := s["const"]; ok && !equal(c, v) {
		bad("must be %s", compact(c))
	}
	switch val := v.(type) {
	case string:
		n := len([]rune(val))
		if m, ok := number(s["minLength"]); ok && float64(n) < m {
			bad("must have at least %g characters", m)
		}
		if m, ok := number(s["maxLength"]); ok && float64(n) > m {
			bad("must have at most %g characters", m)
		}
		if p, ok := s["pattern"].(string); ok {
			if re, err := regexp.Compile(p); err == nil && !re.MatchString(val) {
				bad("must match %s", p)
			}
		}
	case float64:
		if m, ok := number(s["minimum"]); ok && val < m {
			bad("must be at least %g", m)
		}
		if m, ok := number(s["maximum"]); ok && val > m {
			bad("must be at most %g", m)
		}
	case []interface{}:
		if m, ok := number(s["minItems"]); ok && float64(len(val)) < m {
			bad("must have at least %g items", m)
		}
		if m, ok := number(s["maxItems"]); ok && float64(len(val)) > m {
			bad("must have at most %g items", m)
		}
		if items, ok := s["items"].(map[string]interface{}); ok {
			for i, x := range val {
				validate(items, x, fmt.Sprintf("%s[%d]", path, i), out)
			}
		}
	case map[string]interface{}:
		props, _ := s["properties"].(map[string]interface{})
		if req, ok := s["required"].([]interface{}); ok {
			for _, r := range req {
				name, _ := r.(string)
				if _, present := val[name]; !present {
					bad("%q is required", name)
				}
			}
		}
		names := make([]string, 0, len(val))
		for name := range val {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if sub, ok := props[name].(map[string]interface{}); ok {
				validate(sub, val[name], path+"."+name, out)
				continue
			}
			switch ap := s["additionalProperties"].(type) {
			case bool:
				if !ap {
					bad("%q is not allowed", name)
				}
			case map[string]interface{}:
				validate(ap, val[name], path+"."+name, out)
			}
		}
	}
}

func isType(name string, v interface{}) bool {
	switch name {
	case "object":
		_, ok := v.(map[string]interface{})
		return ok
	case "array":
		_, ok := v.([]interface{})
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == math.Trunc(f)
	}
	return false
}

func kind(v interface{}) string {
	switch v.(type) {
	case nil:
		return "null"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	}
	return fmt.Sprintf("%T", v)
}

func number(v interface{}) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func equal(a, b interface{}) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func compact(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
