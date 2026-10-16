package routes_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// Inspect the original bytes before encoding/json can fold struct field names,
// overwrite duplicate members, or replace invalid UTF-8. This is a preflight for
// our finite contract types, not a general OpenAPI or JSON Schema decoder.
// Map keys remain dynamic (paths, properties, headers, security requirements);
// existing document/schema checks constrain their semantics after decoding.
func openAPICheckRawJSON(data []byte, destination any) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("$: invalid UTF-8 JSON input")
	}
	typ := reflect.TypeOf(destination)
	if typ == nil || typ.Kind() != reflect.Pointer || reflect.ValueOf(destination).IsNil() {
		return fmt.Errorf("$: JSON destination must be a non-nil pointer")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := openAPICheckRawValue(decoder, typ.Elem(), "$"); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON content: %v", err)
	}
	return nil
}

func openAPIRawType(typ reflect.Type) reflect.Type {
	for typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ
}

// Only explicitly tagged fields and embedded contract structs belong to the
// supported structural profile. Do not adopt encoding/json's case folding.
func openAPIRawFields(typ reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		embedded := openAPIRawType(field.Type)
		if field.Anonymous && name == "" && embedded.Kind() == reflect.Struct {
			for name, child := range openAPIRawFields(embedded) {
				fields[name] = child
			}
		} else if field.IsExported() && name != "" {
			fields[name] = field.Type
		}
	}
	return fields
}

func openAPICheckRawValue(decoder *json.Decoder, typ reflect.Type, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	typ = openAPIRawType(typ)
	switch token {
	case json.Delim('{'):
		var fields map[string]reflect.Type
		if typ != nil && typ.Kind() == reflect.Struct {
			fields = openAPIRawFields(typ)
		}
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("%s: expected object member", path)
			}
			childPath := fmt.Sprintf("%s[%q]", path, name)
			if seen[name] {
				return fmt.Errorf("%s: duplicate object member", childPath)
			}
			seen[name] = true
			var child reflect.Type
			if fields != nil {
				child, ok = fields[name]
				if !ok {
					return fmt.Errorf("%s: unknown field (exact spelling required)", childPath)
				}
			} else if typ != nil && typ.Kind() == reflect.Map {
				child = typ.Elem()
			}
			if err := openAPICheckRawValue(decoder, child, childPath); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	case json.Delim('['):
		var child reflect.Type
		if typ != nil && (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array) {
			child = typ.Elem()
		}
		for i := 0; decoder.More(); i++ {
			if err := openAPICheckRawValue(decoder, child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}
