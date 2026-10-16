package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// Adapted from the retained independent 032 probe: the eight former schema/Gin
// discrepancies must now be rejected by the request contract and the real router.
func TestOpenAPIA2IntegerRequestBinding(t *testing.T) {
	doc, _ := openAPILoad(t)
	cases := []struct {
		name, method, path, raw string
		status                  int
	}{
		{"create group overflow", "POST", "/api/v1/keys", `{"name":"test","group_id":9223372036854775808}`, 400},
		{"create group underflow", "POST", "/api/v1/keys", `{"name":"test","group_id":-9223372036854775809}`, 400},
		{"update group overflow", "PUT", "/api/v1/keys/{id}", `{"group_id":9223372036854775808}`, 400},
		{"update group underflow", "PUT", "/api/v1/keys/{id}", `{"group_id":-9223372036854775809}`, 400},
		{"expiry int overflow", "POST", "/api/v1/keys", `{"name":"test","expires_in_days":9223372036854775808}`, 400},
		{"expiry int underflow", "POST", "/api/v1/keys", `{"name":"test","expires_in_days":-9223372036854775809}`, 400},
		{"create exponent group", "POST", "/api/v1/keys", `{"name":"test","group_id":11e0}`, 400},
		{"create decimal group", "POST", "/api/v1/keys", `{"name":"test","group_id":11.0}`, 400},
		{"update exponent group", "PUT", "/api/v1/keys/{id}", `{"group_id":11e0}`, 400},
		{"update decimal group", "PUT", "/api/v1/keys/{id}", `{"group_id":11.0}`, 400},
		{"expiry exponent", "POST", "/api/v1/keys", `{"name":"test","expires_in_days":2e0}`, 400},
		{"expiry decimal", "POST", "/api/v1/keys", `{"name":"test","expires_in_days":2.0}`, 400},
		{"ordinary group control", "POST", "/api/v1/keys", `{"name":"test","group_id":11}`, 200},
		{"ordinary update control", "PUT", "/api/v1/keys/{id}", `{"group_id":11}`, 200},
		{"ordinary expiry control", "POST", "/api/v1/keys", `{"name":"test","expires_in_days":2}`, 200},
		{"nullable create fields", "POST", "/api/v1/keys", `{"name":"test","group_id":null,"expires_in_days":null}`, 200},
		{"nullable update field", "PUT", "/api/v1/keys/{id}", `{"group_id":null}`, 200},
		{"unknown decimal still ignored", "POST", "/api/v1/keys", `{"name":"test","future":11.0,"future_object":{"group_id":11e0}}`, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA2NewFixture(t)
			router := f.router(t)
			req := httptest.NewRequest(tc.method, openAPIA2URL(tc.path, "101"), strings.NewReader(tc.raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			op := doc.Paths[tc.path][strings.ToLower(tc.method)]
			contractErr := openAPIValidateRequest(op, req, map[string]string{"id": "101"}, []byte(tc.raw))
			if (contractErr != nil) != (tc.status == 400) {
				t.Fatalf("request contract=%v expected status=%d", contractErr, tc.status)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			t.Logf("contract_error=%v actual_status=%d writes=%d", contractErr, rec.Code, f.keys.writes)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d response=%s", rec.Code, tc.status, rec.Body.String())
			}
			if tc.status == 400 && f.keys.writes != 0 {
				t.Fatal("rejection wrote key")
			}
			if tc.status == 200 && f.keys.writes != 1 {
				t.Fatalf("control writes=%d", f.keys.writes)
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatalf("response contract: %v", err)
			}
		})
	}
}

func TestOpenAPIA2IntegerBindingBoundaries(t *testing.T) {
	// .goreleaser.yaml ships amd64/arm64; .goreleaser.simple.yaml ships amd64.
	// Do not silently claim this 64-bit contract covers a custom 32-bit binary.
	if strconv.IntSize != 64 {
		t.Fatal("A2 request contract targets published 64-bit builds")
	}
	doc, _ := openAPILoad(t)
	for _, field := range []struct{ method, path, name, min string }{
		{"post", "/api/v1/keys", "group_id", "-9223372036854775808"},
		{"put", "/api/v1/keys/{id}", "group_id", "-9223372036854775808"},
		{"post", "/api/v1/keys", "expires_in_days", "1"},
	} {
		t.Run(field.method+"/"+field.name, func(t *testing.T) {
			op := doc.Paths[field.path][field.method]
			props, ok := op.RequestBody.Content["application/json"].Schema["properties"].(map[string]any)
			if !ok {
				t.Fatal("missing properties")
			}
			schema, ok := props[field.name].(map[string]any)
			if !ok {
				t.Fatal("missing field schema")
			}
			for key, want := range map[string]string{"minimum": field.min, "maximum": "9223372036854775807"} {
				got, ok := schema[key].(json.Number)
				if !ok || got.String() != want {
					t.Fatalf("%s=%v want exact %s", key, schema[key], want)
				}
			}
			for _, tc := range []struct {
				token                                   string
				schemaValid, requestValid, bindingValid bool
			}{
				{field.min, true, true, true}, {"9223372036854775807", true, true, true},
				{"9007199254740993", true, true, true},
				{"9223372036854775808", false, false, false}, {"-9223372036854775809", false, false, false},
				{"11.0", true, false, false}, {"11e0", true, false, false}, {"11E+0", true, false, false},
				{"11.5", false, false, false}, {"null", true, true, true},
			} {
				t.Run(tc.token, func(t *testing.T) {
					var value any
					if err := openAPIDecode([]byte(tc.token), &value); err != nil {
						t.Fatal(err)
					}
					schemaErr := openAPIValidateValue(schema, value)
					if (schemaErr == nil) != tc.schemaValid {
						t.Fatalf("schema-only=%v want valid=%v", schemaErr, tc.schemaValid)
					}
					raw := fmt.Sprintf(`{"name":"test",%q:%s}`, field.name, tc.token)
					req := httptest.NewRequest(strings.ToUpper(field.method), openAPIA2URL(field.path, "101"), strings.NewReader(raw))
					req.Header.Set("Content-Type", "application/json")
					requestErr := openAPIValidateRequest(op, req, map[string]string{"id": "101"}, []byte(raw))
					if (requestErr == nil) != tc.requestValid {
						t.Fatalf("request=%v want valid=%v", requestErr, tc.requestValid)
					}
					// Only prove the Gin DTO binding boundary here. Extreme expiry dates and
					// group existence/authorization are subsequent business behavior, not a
					// reason to invent a smaller integer binding range.
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = req
					var dto any = &handler.CreateAPIKeyRequest{}
					if field.method == "put" {
						dto = &handler.UpdateAPIKeyRequest{}
					}
					bindErr := c.ShouldBindJSON(dto)
					if (bindErr == nil) != tc.bindingValid {
						t.Fatalf("Gin binding=%v want valid=%v", bindErr, tc.bindingValid)
					}
				})
			}
		})
	}
}

func TestOpenAPIA2IntegerProfileRemainsFinite(t *testing.T) {
	for _, raw := range []string{
		`{"type":"go-int64"}`,
		`{"type":"integer","format":"int64"}`,
		`{"type":"integer","x-go-integer-token":true}`,
	} {
		var schema map[string]any
		if err := openAPIDecode([]byte(raw), &schema); err != nil {
			t.Fatal(err)
		}
		if err := openAPICheckSchema(schema); err == nil {
			t.Fatalf("unsupported schema silently accepted: %s", raw)
		}
	}
	doc, inv := openAPILoad(t)
	doc.Profile = "a2-typed-map-request-v3"
	if err := openAPIValidateDocument(doc, inv.Normal); err == nil {
		t.Fatal("stale request validation profile accepted")
	}
}
