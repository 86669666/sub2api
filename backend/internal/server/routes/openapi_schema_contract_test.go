package routes_test

import (
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

type openAPIDocument struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title       string `json:"title"`
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"info"`
	Scope      string                                 `json:"x-contract-scope"`
	Baseline   string                                 `json:"x-source-baseline"`
	Profile    string                                 `json:"x-schema-validation-profile"`
	Paths      map[string]map[string]openAPIOperation `json:"paths"`
	Components struct {
		SecuritySchemes map[string]map[string]any `json:"securitySchemes"`
	} `json:"components"`
}
type openAPIOperation struct {
	OperationID string                     `json:"operationId"`
	Summary     string                     `json:"summary"`
	Description string                     `json:"description"`
	Security    *[]map[string][]string     `json:"security"`
	Parameters  []openAPIParameter         `json:"parameters,omitempty"`
	Responses   map[string]openAPIResponse `json:"responses"`
}
type openAPIParameter struct {
	Name     string         `json:"name"`
	In       string         `json:"in"`
	Required bool           `json:"required"`
	Schema   map[string]any `json:"schema"`
}
type openAPIResponse struct {
	Description string `json:"description"`
	Content     map[string]struct {
		Schema map[string]any `json:"schema"`
	} `json:"content,omitempty"`
}

var openAPIID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
var openAPIPathParameter = regexp.MustCompile(`\{([^{}]+)\}`)

func openAPIPath(ginPath string) string {
	parts := strings.Split(ginPath, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}
func openAPIExpectedSecurity(auth string) ([]map[string][]string, error) {
	switch auth {
	case "public":
		return []map[string][]string{}, nil
	case "jwt":
		return []map[string][]string{{"BearerAuth": {}}}, nil
	case "optional-jwt":
		return []map[string][]string{{}, {"BearerAuth": {}}}, nil
	case "admin-jwt-or-key":
		return []map[string][]string{{"BearerAuth": {}}, {"AdminAPIKey": {}}}, nil
	case "gateway-key", "google-gateway-key":
		return []map[string][]string{{"BearerAuth": {}}, {"APIKey": {}}, {"GoogleAPIKey": {}}}, nil
	default:
		return nil, fmt.Errorf("authentication %q has not been reviewed for OpenAPI coverage", auth)
	}
}
func openAPISecurityKey(requirements []map[string][]string) string {
	alternatives := []string{}
	for _, requirement := range requirements {
		data, _ := json.Marshal(requirement)
		alternatives = append(alternatives, string(data))
	}
	sort.Strings(alternatives)
	return strings.Join(alternatives, "|")
}
func openAPIValidateDocument(doc openAPIDocument, inventory []openAPICoverage) error {
	if doc.OpenAPI != "3.1.1" || doc.Info.Title == "" || doc.Info.Version == "" || doc.Scope != "normal-mode A0 partial" || doc.Profile != "a0-closed-object-v1" {
		return fmt.Errorf("invalid OAS/version/scope/profile metadata")
	}
	if len(doc.Paths) == 0 {
		return fmt.Errorf("no covered operations")
	}
	known := map[string]openAPICoverage{}
	for _, entry := range inventory {
		key := entry.Method + " " + openAPIPath(entry.Path)
		if _, exists := known[key]; exists {
			return fmt.Errorf("ambiguous normalized path: %s", key)
		}
		known[key] = entry
	}
	ids := map[string]bool{}
	seen := map[string]bool{}
	for path, methods := range doc.Paths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("path must start with slash: %s", path)
		}
		for method, op := range methods {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				return fmt.Errorf("unsupported operation key %s", method)
			}
			key := strings.ToUpper(method) + " " + path
			route, exists := known[key]
			if !exists {
				return fmt.Errorf("documented operation is not registered: %s", key)
			}
			if route.Coverage != "covered" {
				return fmt.Errorf("operation has not been accepted as covered: %s", key)
			}
			if !openAPIID.MatchString(op.OperationID) || ids[op.OperationID] {
				return fmt.Errorf("missing/invalid/duplicate operationId: %q", op.OperationID)
			}
			ids[op.OperationID] = true
			if op.Security == nil {
				return fmt.Errorf("explicit per-operation security missing: %s", key)
			}
			expected, err := openAPIExpectedSecurity(route.Authentication)
			if err != nil {
				return err
			}
			if openAPISecurityKey(*op.Security) != openAPISecurityKey(expected) {
				return fmt.Errorf("authentication mislabel: %s (%s)", key, route.Authentication)
			}
			for _, requirement := range *op.Security {
				for scheme, scopes := range requirement {
					if len(scopes) != 0 {
						return fmt.Errorf("unexpected OAuth scopes: %s", scheme)
					}
					if _, ok := doc.Components.SecuritySchemes[scheme]; !ok {
						return fmt.Errorf("undefined security scheme: %s", scheme)
					}
				}
			}
			wantParams := map[string]bool{}
			for _, match := range openAPIPathParameter.FindAllStringSubmatch(path, -1) {
				if wantParams[match[1]] {
					return fmt.Errorf("duplicate template parameter: %s", path)
				}
				wantParams[match[1]] = true
			}
			gotParams := map[string]bool{}
			for _, p := range op.Parameters {
				if p.In != "path" || !p.Required || !wantParams[p.Name] || gotParams[p.Name] {
					return fmt.Errorf("invalid/mismatched path parameter: %s %s", key, p.Name)
				}
				if err := openAPICheckSchema(p.Schema); err != nil {
					return err
				}
				gotParams[p.Name] = true
			}
			if !reflect.DeepEqual(wantParams, gotParams) {
				return fmt.Errorf("missing path parameters: %s", key)
			}
			if len(op.Responses) == 0 {
				return fmt.Errorf("no responses: %s", key)
			}
			for status, response := range op.Responses {
				if !regexp.MustCompile(`^[1-5][0-9][0-9]$`).MatchString(status) || response.Description == "" {
					return fmt.Errorf("invalid status/description: %s %s", key, status)
				}
				for media, body := range response.Content {
					if media != "application/json" {
						return fmt.Errorf("unsupported A0 response media type: %s", media)
					}
					if err := openAPICheckSchema(body.Schema); err != nil {
						return fmt.Errorf("%s %s: %w", key, status, err)
					}
				}
			}
			seen[key] = true
		}
	}
	for key, route := range known {
		if route.Coverage == "covered" && !seen[key] {
			return fmt.Errorf("covered route missing from document: %s", key)
		}
	}
	// Scheme definitions are machine-checked, not just names that can be relabeled.
	expectedSchemes := map[string]map[string]any{
		"BearerAuth":   {"type": "http", "scheme": "bearer"},
		"AdminAPIKey":  {"type": "apiKey", "in": "header", "name": "x-api-key"},
		"APIKey":       {"type": "apiKey", "in": "header", "name": "x-api-key"},
		"GoogleAPIKey": {"type": "apiKey", "in": "header", "name": "x-goog-api-key"},
	}
	if !reflect.DeepEqual(doc.Components.SecuritySchemes, expectedSchemes) {
		return fmt.Errorf("security scheme definition drift")
	}
	return nil
}

// A deliberately small, fail-closed JSON Schema profile for the A0 operations.
// This is NOT a general OAS/JSON Schema validator. Unsupported keywords fail the
// gate instead of silently passing: extend it with tests or adopt a reviewed
// validator before expanding the schema dialect in later slices.
func openAPICheckSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return fmt.Errorf("empty schema is not coverage")
	}
	for key := range schema {
		switch key {
		case "type", "properties", "required", "additionalProperties", "const", "description":
		default:
			return fmt.Errorf("unsupported A0 schema keyword: %s", key)
		}
	}
	typ, ok := schema["type"].(string)
	if !ok {
		return fmt.Errorf("schema requires an explicit type")
	}
	switch typ {
	case "object":
		if schema["additionalProperties"] != false {
			return fmt.Errorf("A0 objects must be closed")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok || len(props) == 0 {
			return fmt.Errorf("object properties missing")
		}
		required, ok := schema["required"].([]any)
		if !ok {
			return fmt.Errorf("object required list missing")
		}
		seen := map[string]bool{}
		for _, value := range required {
			name, ok := value.(string)
			if !ok || seen[name] {
				return fmt.Errorf("invalid required property")
			}
			if _, ok := props[name]; !ok {
				return fmt.Errorf("unknown required property: %s", name)
			}
			seen[name] = true
		}
		for name, value := range props {
			child, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid property schema: %s", name)
			}
			if err := openAPICheckSchema(child); err != nil {
				return err
			}
		}
	case "string", "boolean", "integer":
		for _, key := range []string{"properties", "required", "additionalProperties"} {
			if _, ok := schema[key]; ok {
				return fmt.Errorf("object keyword on %s", typ)
			}
		}
	default:
		return fmt.Errorf("unsupported A0 schema type: %s", typ)
	}
	if constant, ok := schema["const"]; ok {
		copy := map[string]any{}
		for k, v := range schema {
			if k != "const" {
				copy[k] = v
			}
		}
		if err := openAPIValidateValue(copy, constant); err != nil {
			return fmt.Errorf("invalid const: %w", err)
		}
	}
	return nil
}
func openAPIValidateValue(schema map[string]any, value any) error {
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		props := schema["properties"].(map[string]any)
		for _, raw := range schema["required"].([]any) {
			name := raw.(string)
			if _, ok := object[name]; !ok {
				return fmt.Errorf("missing required %s", name)
			}
		}
		for name, v := range object {
			p, ok := props[name]
			if !ok {
				return fmt.Errorf("unexpected property %s", name)
			}
			if err := openAPIValidateValue(p.(map[string]any), v); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean")
		}
	case "integer":
		n, ok := value.(float64)
		if !ok || math.Trunc(n) != n {
			return fmt.Errorf("expected integer")
		}
	default:
		return fmt.Errorf("unvalidated schema type")
	}
	if constant, ok := schema["const"]; ok && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("constant mismatch: expected %v got %v", constant, value)
	}
	return nil
}
func openAPIValidateResponse(op openAPIOperation, rec *httptest.ResponseRecorder) error {
	response, ok := op.Responses[fmt.Sprint(rec.Code)]
	if !ok {
		return fmt.Errorf("undocumented HTTP status %d", rec.Code)
	}
	if len(response.Content) == 0 {
		if rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
			return fmt.Errorf("expected empty response without media type")
		}
		return nil
	}
	media, _, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if err != nil {
		return err
	}
	body, ok := response.Content[media]
	if !ok {
		return fmt.Errorf("undocumented response media %s", media)
	}
	if err := openAPICheckSchema(body.Schema); err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		return err
	}
	return openAPIValidateValue(body.Schema, value)
}
func TestOpenAPIDocument(t *testing.T) {
	var doc openAPIDocument
	var inventory openAPIInventory
	openAPIRead(t, "openapi.json", &doc)
	openAPIRead(t, "route-inventory.json", &inventory)
	if doc.Baseline != inventory.Baseline {
		t.Fatal("source baseline mismatch")
	}
	if err := openAPIValidateDocument(doc, inventory.Normal); err != nil {
		t.Fatal(err)
	}
}
func TestOpenAPIResponses(t *testing.T) {
	var doc openAPIDocument
	openAPIRead(t, "openapi.json", &doc)
	r, _ := openAPIRouter(t, false, true)
	for path, methods := range doc.Paths {
		for method, op := range methods {
			t.Run(method+" "+path, func(t *testing.T) {
				key := strings.ToUpper(method) + " " + path
				if !openAPICommon[key] {
					t.Fatalf("new operation needs an isolated executable response fixture: %s", key)
				}
				body := ""
				if method == "post" {
					body = `{"ignored":"fixture"}`
				}
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(strings.ToUpper(method), path, strings.NewReader(body))
				if body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				r.ServeHTTP(rec, req)
				if err := openAPIValidateResponse(op, rec); err != nil {
					t.Fatal(err)
				}
				t.Logf("actual handler response: HTTP %d content-type=%q body=%s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			})
		}
	}
}
