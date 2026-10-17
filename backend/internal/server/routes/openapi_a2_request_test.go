package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// Request DTO int/int64 fields use encoding/json integer tokens. This is a
// binding rule for the declared top-level request fields, not JSON Schema's
// mathematical integer semantics. Numeric ranges remain in minimum/maximum.
var openAPIGoIntegerToken = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// The only open object supported is a declared request DTO: production ignores
// unknown JSON members. Responses and the document itself remain fail-closed.
func openAPICheckRequestSchema(schema map[string]any) error {
	if schema["type"] != "object" || schema["additionalProperties"] != true {
		return fmt.Errorf("request must declare known object fields and ignored extras")
	}
	copy := map[string]any{}
	for k, v := range schema {
		copy[k] = v
	}
	copy["additionalProperties"] = false
	return openAPICheckSchema(copy)
}
func openAPIValidateRequest(op openAPIOperation, req *http.Request, pathValues map[string]string, body []byte) error {
	for _, p := range op.Parameters {
		var value string
		var present bool
		if err := openAPICheckParameter(p); err != nil {
			return err
		}
		if p.Schema["type"] == "array" {
			if err := openAPIValidateQueryList(p, req.URL.Query()); err != nil {
				return err
			}
			continue
		}
		switch p.In {
		case "path":
			value, present = pathValues[p.Name]
		case "query":
			values, ok := req.URL.Query()[p.Name]
			present = ok
			if len(values) > 0 {
				value = values[0]
			}
		case "header":
			value = req.Header.Get(p.Name)
			present = value != ""
		default:
			return fmt.Errorf("unvalidated parameter location")
		}
		if !present {
			if p.Required {
				return fmt.Errorf("missing parameter %s", p.Name)
			}
			continue
		}
		if err := openAPICheckSchema(p.Schema); err != nil {
			return err
		}
		if err := openAPIValidateValue(p.Schema, value); err != nil {
			return fmt.Errorf("%s: %w", p.Name, err)
		}
	}
	if op.RequestBody == nil {
		return nil
	}
	if len(body) == 0 {
		return fmt.Errorf("missing request body")
	}
	schema, ok := op.RequestBody.Content["application/json"]
	if !ok {
		return fmt.Errorf("unsupported request schema")
	}
	if err := openAPICheckRequestSchema(schema.Schema); err != nil {
		return err
	}
	var value map[string]any
	// Integrity of saved test input is stricter than production binding: production
	// tolerance is explicitly exercised separately; it must not weaken raw decoding.
	if err := openAPIDecode(body, &value); err != nil {
		return err
	}
	props, ok := schema.Schema["properties"].(map[string]any)
	if !ok {
		return fmt.Errorf("unvalidated request properties")
	}
	known := map[string]any{}
	for k, v := range value {
		if _, ok := props[k]; ok {
			known[k] = v
		}
	}
	closed := map[string]any{}
	for k, v := range schema.Schema {
		closed[k] = v
	}
	closed["additionalProperties"] = false
	if err := openAPIValidateValue(closed, known); err != nil {
		return err
	}
	for name, value := range known {
		field, ok := props[name].(map[string]any)
		if !ok {
			return fmt.Errorf("unvalidated request field %s", name)
		}
		if err := openAPIValidateRequestIntegerTokens(field, value); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}
func openAPIA2Operation(key string) bool {
	switch key {
	case "GET /api/v1/keys", "POST /api/v1/keys", "GET /api/v1/keys/{id}", "PUT /api/v1/keys/{id}", "DELETE /api/v1/keys/{id}", "GET /api/v1/groups/available", "GET /api/v1/groups/rates":
		return true
	}
	return false
}
func openAPIA2URL(path, id string) string { return strings.ReplaceAll(path, "{id}", id) }

// Request DTO arrays of Go integers retain the same binding restriction as
// scalar fields. Null elements bind the zero value; schema bounds remain separate.
func openAPIValidateRequestIntegerTokens(schema map[string]any, value any) error {
	if value == nil {
		return nil
	}
	typ, _, err := openAPISchemaType(schema)
	if err != nil {
		return err
	}
	switch typ {
	case "integer":
		n, ok := value.(json.Number)
		if !ok || !openAPIGoIntegerToken.MatchString(n.String()) {
			return fmt.Errorf("expected Go integer JSON token without decimal point or exponent")
		}
	case "array":
		items, ok := schema["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("unvalidated array items")
		}
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		for _, v := range values {
			if err := openAPIValidateRequestIntegerTokens(items, v); err != nil {
				return err
			}
		}
	}
	return nil
}
