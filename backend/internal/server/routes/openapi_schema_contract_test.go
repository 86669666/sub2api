package routes_test

import (
	"encoding/json"
	"fmt"
	"math/big"
	"mime"
	"net/http/httptest"
	"reflect"
	"regexp"
	"sort"
	"strconv"
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
type openAPIHeader struct {
	Description string         `json:"description"`
	Required    bool           `json:"required"`
	Schema      map[string]any `json:"schema"`
}
type openAPIResponse struct {
	Headers     map[string]openAPIHeader `json:"headers,omitempty"`
	Description string                   `json:"description"`
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
		return []map[string][]string{{"BearerAuth": {}}, {"AdminAPIKey": {}}, {"AdminUpgradeJWT": {}}}, nil
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
	if doc.OpenAPI != "3.1.1" || doc.Info.Title == "" || doc.Info.Version == "" || doc.Scope != "normal-mode A0+A1 partial" || doc.Profile != "a1-closed-object-array-v2" {
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
				for name, header := range response.Headers {
					if name != "Retry-After" || status != "429" || header.Description == "" || !header.Required || len(header.Schema) != 1 || header.Schema["type"] != "string" {
						return fmt.Errorf("unsupported response header contract")
					}
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
		"BearerAuth":      {"type": "http", "scheme": "bearer"},
		"AdminUpgradeJWT": {"type": "apiKey", "in": "header", "name": "Sec-WebSocket-Protocol"},
		"AdminAPIKey":     {"type": "apiKey", "in": "header", "name": "x-api-key"},
		"APIKey":          {"type": "apiKey", "in": "header", "name": "x-api-key"},
		"GoogleAPIKey":    {"type": "apiKey", "in": "header", "name": "x-goog-api-key"},
	}
	if !reflect.DeepEqual(doc.Components.SecuritySchemes, expectedSchemes) {
		return fmt.Errorf("security scheme definition drift")
	}
	return nil
}

// A deliberately small, fail-closed JSON Schema profile for A0+A1.
// A1 adds homogeneous arrays, array-or-null and exact numbers, not general unions.
// This is NOT a general OAS/JSON Schema validator. Unsupported keywords fail the
// gate instead of silently passing: extend it with tests or adopt a reviewed
// validator before expanding the schema dialect in later slices.
func openAPISchemaType(schema map[string]any) (string, bool, error) {
	if typ, ok := schema["type"].(string); ok {
		return typ, false, nil
	}
	if types, ok := schema["type"].([]any); ok {
		if len(types) == 2 && ((types[0] == "array" && types[1] == "null") || (types[0] == "null" && types[1] == "array")) {
			return "array", true, nil
		}
		return "", false, fmt.Errorf("unsupported type union; only array-or-null is supported")
	}
	return "", false, fmt.Errorf("schema requires an explicit type")
}

func openAPICheckSchema(schema map[string]any) error {
	if len(schema) == 0 {
		return fmt.Errorf("empty schema is not coverage")
	}
	for key := range schema {
		switch key {
		case "type", "properties", "required", "additionalProperties", "const", "description", "items":
		default:
			return fmt.Errorf("unsupported A0 schema keyword: %s", key)
		}
	}
	typ, _, err := openAPISchemaType(schema)
	if err != nil {
		return err
	}
	if d, exists := schema["description"]; exists {
		if _, ok := d.(string); !ok {
			return fmt.Errorf("description must be a string")
		}
	}
	if typ != "array" {
		if _, exists := schema["items"]; exists {
			return fmt.Errorf("array keyword on %s", typ)
		}
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
	case "array", "string", "boolean", "integer", "number":
		for _, key := range []string{"properties", "required", "additionalProperties"} {
			if _, ok := schema[key]; ok {
				return fmt.Errorf("object keyword on %s", typ)
			}
		}
		if typ == "array" {
			items, ok := schema["items"].(map[string]any)
			if !ok {
				return fmt.Errorf("array requires one item schema")
			}
			if err := openAPICheckSchema(items); err != nil {
				return fmt.Errorf("items: %w", err)
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

// Exact rational arithmetic within a deliberately bounded decimal profile.
// Reject unsupported sizes BEFORE constructing powers of ten. These limits
// apply equally to schema constants and responses; no float conversion occurs.
func openAPIExactNumber(value any) (*big.Rat, error) {
	n, ok := value.(json.Number)
	if !ok {
		return nil, fmt.Errorf("expected integer encoded as an exact JSON number")
	}
	text := n.String()
	if len(text) > 1024 || !json.Valid([]byte(text)) {
		return nil, fmt.Errorf("unsupported A0 number encoding or length")
	}
	if at := strings.IndexAny(text, "eE"); at >= 0 {
		exponent, err := strconv.ParseInt(text[at+1:], 10, 32)
		if err != nil || exponent < -4096 || exponent > 4096 {
			return nil, fmt.Errorf("unsupported A0 number exponent")
		}
	}
	exact, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil, fmt.Errorf("unsupported A0 number encoding")
	}
	return exact, nil
}

// Object constants may contain numeric children. Compare those recursively by
// mathematical value too, so equivalent decimal/exponent encodings agree.
func openAPIEqualValue(a, b any) bool {
	switch a := a.(type) {
	case json.Number:
		x, err := openAPIExactNumber(a)
		if err != nil {
			return false
		}
		y, err := openAPIExactNumber(b)
		return err == nil && x.Cmp(y) == 0
	case []any:
		other, ok := b.([]any)
		if !ok || len(a) != len(other) {
			return false
		}
		for i := range a {
			if !openAPIEqualValue(a[i], other[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		other, ok := b.(map[string]any)
		if !ok || len(a) != len(other) {
			return false
		}
		for k, v := range a {
			w, exists := other[k]
			if !exists || !openAPIEqualValue(v, w) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}

func openAPIValidateValue(schema map[string]any, value any) error {
	typ, nullable, err := openAPISchemaType(schema)
	if err != nil {
		return err
	}
	if value == nil && nullable {
		if constant, ok := schema["const"]; ok && constant != nil {
			return fmt.Errorf("constant mismatch")
		}
		return nil
	}
	switch typ {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object")
		}
		props, ok := schema["properties"].(map[string]any)
		if !ok {
			return fmt.Errorf("unvalidated object properties")
		}
		required, ok := schema["required"].([]any)
		if !ok {
			return fmt.Errorf("unvalidated required fields")
		}
		for _, raw := range required {
			name, ok := raw.(string)
			if !ok {
				return fmt.Errorf("unvalidated required field name")
			}
			if _, ok := object[name]; !ok {
				return fmt.Errorf("missing required %s", name)
			}
		}
		for name, v := range object {
			p, ok := props[name]
			if !ok {
				return fmt.Errorf("unexpected property %s", name)
			}
			propertySchema, ok := p.(map[string]any)
			if !ok {
				return fmt.Errorf("unvalidated property schema %s", name)
			}
			if err := openAPIValidateValue(propertySchema, v); err != nil {
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
	case "array":
		items, ok := schema["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("unvalidated array items")
		}
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array")
		}
		for i, v := range values {
			if err := openAPIValidateValue(items, v); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case "number":
		if _, err := openAPIExactNumber(value); err != nil {
			return err
		}
	case "integer":
		n, err := openAPIExactNumber(value)
		if err != nil {
			return err
		}
		if !n.IsInt() {
			return fmt.Errorf("expected integer")
		}
	default:
		return fmt.Errorf("unvalidated schema type")
	}
	if constant, ok := schema["const"]; ok && !openAPIEqualValue(constant, value) {
		return fmt.Errorf("constant mismatch: expected %v got %v", constant, value)
	}
	return nil
}
func openAPIValidateResponse(op openAPIOperation, rec *httptest.ResponseRecorder) error {
	response, ok := op.Responses[fmt.Sprint(rec.Code)]
	if !ok {
		return fmt.Errorf("undocumented HTTP status %d", rec.Code)
	}
	for name, header := range response.Headers {
		value := rec.Header().Get(name)
		if header.Required && value == "" {
			return fmt.Errorf("missing response header %s", name)
		}
		if name == "Retry-After" {
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid Retry-After seconds")
			}
		}
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
	if err := openAPIDecode(rec.Body.Bytes(), &value); err != nil {
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
					openAPIA1VerifyResponses(t, path, op)
					return
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
