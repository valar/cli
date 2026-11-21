package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Error is an invalid service configuration file.
type Error struct {
	Path string
	Err  error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Schema returns the JSON Schema of .valar.yml, derived from ServiceConfigYAML.
func Schema() map[string]any {
	schema := schemaOf(reflect.TypeOf(ServiceConfigYAML{}))
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["title"] = "Valar service configuration (.valar.yml)"
	return schema
}

func (EnvironmentConfig) jsonSchema() map[string]any {
	return map[string]any{
		"oneOf": []any{
			map[string]any{
				"type":        "string",
				"pattern":     "^[^=]+=",
				"description": "A plain variable as KEY=VALUE.",
			},
			map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"key":    map[string]any{"type": "string"},
					"value":  map[string]any{"type": "string"},
					"secret": map[string]any{"type": "boolean"},
				},
			},
		},
	}
}

type schemaProvider interface{ jsonSchema() map[string]any }

func schemaOf(t reflect.Type) map[string]any {
	if p, ok := reflect.Zero(t).Interface().(schemaProvider); ok {
		return p.jsonSchema()
	}
	switch t.Kind() {
	case reflect.Pointer:
		schema := schemaOf(t.Elem())
		schema["type"] = []any{schema["type"], "null"}
		return schema
	case reflect.Slice:
		return map[string]any{"type": []any{"array", "null"}, "items": schemaOf(t.Elem())}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Struct:
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
			if !field.IsExported() || name == "-" || name == "" {
				continue
			}
			prop := schemaOf(field.Type)
			if doc := field.Tag.Get("doc"); doc != "" {
				prop["description"] = doc
			}
			if strings.HasPrefix(field.Tag.Get("doc"), "Deprecated") {
				prop["deprecated"] = true
			}
			props[name] = prop
		}
		return map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	}
	panic(fmt.Sprintf("no schema for %s", t))
}

// validate refuses keys that schema does not know. Value types are left to
// the decoder.
func validate(doc *yaml.Node, schema map[string]any) error {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		return validateNode(doc.Content[0], schema, "")
	}
	return nil
}

func validateNode(node *yaml.Node, schema map[string]any, at string) error {
	if node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	if branches, ok := schema["oneOf"].([]any); ok {
		for _, branch := range branches {
			b := branch.(map[string]any)
			if (node.Kind == yaml.MappingNode) == (b["type"] == "object") {
				return validateNode(node, b, at)
			}
		}
		return nil
	}
	switch node.Kind {
	case yaml.MappingNode:
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			return nil
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Value == "<<" {
				continue
			}
			prop, known := props[key.Value].(map[string]any)
			if !known {
				return unknownKeyError(key, at, props)
			}
			if err := validateNode(value, prop, join(at, key.Value)); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		items, ok := schema["items"].(map[string]any)
		if !ok {
			return nil
		}
		for i, item := range node.Content {
			if err := validateNode(item, items, fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func unknownKeyError(key *yaml.Node, at string, props map[string]any) error {
	known := make([]string, 0, len(props))
	for name := range props {
		known = append(known, name)
	}
	sort.Strings(known)
	where := "at the top level"
	if at != "" {
		where = "in " + at
	}
	return fmt.Errorf("line %d: unknown key %q %s, known keys are %s", key.Line, key.Value, where, strings.Join(known, ", "))
}

func join(at, key string) string {
	if at == "" {
		return key
	}
	return at + "." + key
}
