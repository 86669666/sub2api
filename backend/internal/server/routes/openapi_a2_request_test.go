package routes_test

import (
	"fmt"
	"net/http"
	"strings"
)

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
	return openAPIValidateValue(closed, known)
}
func openAPIA2Operation(key string) bool {
	switch key {
	case "GET /api/v1/keys", "POST /api/v1/keys", "GET /api/v1/keys/{id}", "PUT /api/v1/keys/{id}", "DELETE /api/v1/keys/{id}", "GET /api/v1/groups/available", "GET /api/v1/groups/rates":
		return true
	}
	return false
}
func openAPIA2URL(path, id string) string { return strings.ReplaceAll(path, "{id}", id) }
