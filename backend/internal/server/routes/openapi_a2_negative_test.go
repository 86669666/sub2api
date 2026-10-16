package routes_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestOpenAPIA2FiniteExtensions(t *testing.T) {
	for _, tc := range []struct {
		name, schema, value string
		valid               bool
	}{
		{"nullable integer", `{"type":["integer","null"]}`, `null`, true},
		{"nullable string", `{"type":["string","null"]}`, `"text"`, true},
		{"nullable boolean type", `{"type":["boolean","null"]}`, `1`, false},
		{"enum null excluded", `{"type":["string","null"],"enum":["active"]}`, `null`, false},
		{"enum null included", `{"type":["string","null"],"enum":["active",null]}`, `null`, true},
		{"finite mixed code", `{"type":["integer","string"],"enum":[401,"INVALID_TOKEN"]}`, `"INVALID_TOKEN"`, true},
		{"mixed code wrong", `{"type":["integer","string"],"enum":[401,"INVALID_TOKEN"]}`, `"OTHER"`, false},
		{"literal synthetic code type rejected", `{"type":"code","enum":[401]}`, `401`, false},
		{"mixed code needs enum", `{"type":["integer","string"]}`, `401`, false},
		{"duplicate exact enum", `{"type":"number","enum":[1,1e0]}`, `1`, false},
		{"invalid enum type", `{"type":"integer","enum":["1"]}`, `1`, false},
		{"exact boundary", `{"type":"integer","minimum":9007199254740993,"maximum":9007199254740993}`, `9007199254740993`, true},
		{"below exact boundary", `{"type":"integer","minimum":9007199254740993}`, `9007199254740992`, false},
		{"above exact boundary", `{"type":"number","maximum":0.125}`, `0.12500000000000001`, false},
		{"inverted numeric bounds", `{"type":"number","minimum":2,"maximum":1}`, `1`, false},
		{"bounds wrong type", `{"type":"string","minimum":0}`, `"x"`, false},
		{"unicode length", `{"type":"string","minLength":2,"maxLength":2}`, `"图😀"`, true},
		{"length short", `{"type":"string","minLength":2}`, `"图"`, false},
		{"invalid length", `{"type":"string","minLength":0.5}`, `"x"`, false},
		{"timestamp", `{"type":"string","format":"date-time"}`, `"2035-01-02T03:04:05Z"`, true},
		{"timestamp invalid", `{"type":"string","format":"date-time"}`, `"tomorrow"`, false},
		{"int64 path", `{"type":"string","format":"int64"}`, `"+00101"`, true},
		{"int64 overflow", `{"type":"string","format":"int64"}`, `"9223372036854775808"`, false},
		{"unsupported format", `{"type":"string","format":"email"}`, `"a@b"`, false},
		{"typed map", `{"type":["object","null"],"properties":{},"required":[],"propertyNames":{"type":"string","format":"int64"},"additionalProperties":{"type":"number"}}`, `{"11":0.125}`, true},
		{"typed map null", `{"type":["object","null"],"properties":{},"required":[],"additionalProperties":{"type":"number"}}`, `null`, true},
		{"typed map wrong value", `{"type":"object","properties":{},"required":[],"additionalProperties":{"type":"number"}}`, `{"11":"0.125"}`, false},
		{"typed map wrong name", `{"type":"object","properties":{},"required":[],"propertyNames":{"type":"string","format":"int64"},"additionalProperties":{"type":"number"}}`, `{"secret":0.125}`, false},
		{"empty map value schema", `{"type":"object","properties":{},"required":[],"additionalProperties":{}}`, `{}`, false},
		{"untyped open response remains rejected", `{"type":"object","properties":{"x":{"type":"string"}},"required":[],"additionalProperties":true}`, `{}`, false},
		{"unsupported pattern remains rejected", `{"type":"string","pattern":"x"}`, `"x"`, false},
		{"arbitrary union rejected", `{"type":["integer","boolean"]}`, `1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema map[string]any
			var value any
			if err := openAPIDecode([]byte(tc.schema), &schema); err != nil {
				t.Fatal(err)
			}
			if err := openAPIDecode([]byte(tc.value), &value); err != nil {
				t.Fatal(err)
			}
			err := openAPICheckSchema(schema)
			if err == nil {
				err = openAPIValidateValue(schema, value)
			}
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
func TestOpenAPIA2CoverageAndRequestContracts(t *testing.T) {
	doc, inv := openAPILoad(t)
	count := 0
	for _, entry := range inv.Normal {
		key := entry.Method + " " + openAPIPath(entry.Path)
		if openAPIA2Operation(key) {
			count++
			if entry.Coverage != "covered" || entry.Authentication != "jwt" {
				t.Fatalf("A2 coverage/auth demoted: %s", key)
			}
			if doc.Paths[openAPIPath(entry.Path)][strings.ToLower(entry.Method)].OperationID == "" {
				t.Fatal("missing module operation")
			}
		}
	}
	if count != 7 {
		t.Fatalf("A2 denominator %d", count)
	}
	op := doc.Paths["/api/v1/keys"]["post"]
	for _, raw := range []string{`{"name":"ok","future":{"Headers":1}}`, `{"name":" "}`, `{"name":"图","quota":0.125,"group_id":null}`} {
		req := httptest.NewRequest("POST", "/api/v1/keys", nil)
		if err := openAPIValidateRequest(op, req, nil, []byte(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{`{"name":"first","name":"second"}`, "{\"name\":\"\xff\"}", `{"name":"ok"} {}`, `{"name":false}`, `{"name":"ok","quota":-1}`} {
		if err := openAPIValidateRequest(op, httptest.NewRequest("POST", "/api/v1/keys", nil), nil, []byte(raw)); err == nil {
			t.Fatalf("invalid request passed: %q", raw)
		}
	}
	for _, mutate := range []func(*openAPIRequestBody){func(b *openAPIRequestBody) { b.Required = false }, func(b *openAPIRequestBody) { b.Content["text/plain"] = b.Content["application/json"] }, func(b *openAPIRequestBody) {
		b.Content["application/json"].Schema["additionalProperties"] = map[string]any{}
	}, func(b *openAPIRequestBody) { b.Content["application/json"].Schema["unknown_keyword"] = true }} {
		copy := openAPIClone(t, doc)
		o := copy.Paths["/api/v1/keys"]["post"]
		mutate(o.RequestBody)
		if err := openAPIValidateDocument(copy, inv.Normal); err == nil {
			t.Fatal("unsupported request contract accepted")
		}
	}
}
func TestOpenAPIA2ResponseMutations(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/api/v1/keys/{id}"]["get"]
	f := openAPIA2NewFixture(t)
	r := f.router(t)
	req := httptest.NewRequest("GET", "/api/v1/keys/101", nil)
	req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var body map[string]any
	if err := openAPIDecode(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	validate := func(t *testing.T, b map[string]any) {
		t.Helper()
		v := httptest.NewRecorder()
		v.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(v).Encode(b); err != nil {
			t.Fatal(err)
		}
		if err := openAPIValidateResponse(op, v); err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	schema := op.Responses["200"].Content["application/json"].Schema
	props := openAPIA1As[map[string]any](t, schema["properties"])
	dataSchema := openAPIA1As[map[string]any](t, props["data"])
	for _, raw := range openAPIA1As[[]any](t, dataSchema["required"]) {
		name := openAPIA1As[string](t, raw)
		t.Run("missing "+name, func(t *testing.T) {
			b := openAPIClone(t, body)
			delete(openAPIA1As[map[string]any](t, b["data"]), name)
			validate(t, b)
		})
	}
	for _, name := range []string{"admin_notes", "credentials", "password_hash", "profit_min_margin"} {
		t.Run("reject secret "+name, func(t *testing.T) {
			b := openAPIClone(t, body)
			openAPIA1As[map[string]any](t, b["data"])[name] = "synthetic-secret-canary"
			validate(t, b)
		})
	}
	b := openAPIClone(t, body)
	openAPIA1As[map[string]any](t, b["data"])["quota"] = "0.125"
	validate(t, b)
}
func TestOpenAPIA2IdempotencyReplayAndConflict(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/api/v1/keys"]["post"]
	f := openAPIA2NewFixture(t)
	f.enforceIdem = true
	r := f.router(t)
	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/api/v1/keys", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "a2-synthetic-idempotent")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if err := openAPIValidateResponse(op, rec); err != nil {
			t.Fatalf("contract: %v %s", err, rec.Body.String())
		}
		return rec
	}
	first := call(`{"name":"same","custom_key":"` + openAPIA2CreateKey + `"}`)
	second := call(`{"name":"same","custom_key":"` + openAPIA2CreateKey + `"}`)
	if first.Code != 200 || second.Code != 200 || second.Header().Get("X-Idempotency-Replayed") != "true" || !openAPIA2SameJSON(t, first.Body.Bytes(), second.Body.Bytes()) || f.keys.writes != 1 {
		t.Fatal("replay did not preserve result or duplicated write")
	}
	conflict := call(`{"name":"changed"}`)
	if conflict.Code != 409 || f.keys.writes != 1 {
		t.Fatal("conflicting payload wrote")
	}
}
func TestOpenAPIA2ProductionBindingTolerance(t *testing.T) {
	// Document the real decoder tolerance without weakening saved-JSON integrity.
	for _, raw := range []string{`{"name":"first","name":"last"}`, `{"name":"ok"} {"extra":true}`, "{\"name\":\"\xff\"}"} {
		t.Run(raw, func(t *testing.T) {
			f := openAPIA2NewFixture(t)
			req := httptest.NewRequest("POST", "/api/v1/keys", strings.NewReader(raw))
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			f.router(t).ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("production tolerance changed: %d %s", rec.Code, rec.Body.String())
			}
			if f.keys.writes != 1 {
				t.Fatal("handler not executed")
			}
		})
	}
}

func openAPIA2SameJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := openAPIDecode(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := openAPIDecode(b, &y); err != nil {
		t.Fatal(err)
	}
	return openAPIEqualValue(x, y)
}
