package routes_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIA3FiniteArrayBound(t *testing.T) {
	for _, tc := range []struct {
		schema, value string
		valid         bool
	}{
		{`{"type":"array","items":{"type":"integer"},"maxItems":0}`, `[]`, true},
		{`{"type":"array","items":{"type":"integer"},"maxItems":0}`, `[1]`, false},
		{`{"type":"array","items":{"type":"integer"},"maxItems":1}`, `[1e0]`, true},
		{`{"type":"array","items":{"type":"integer"},"maxItems":1}`, `[1,2]`, false},
		{`{"type":"array","items":{"type":"integer"},"maxItems":1,"enum":[[1],[2]]}`, `[1]`, true},
		{`{"type":"array","items":{"type":"integer"},"maxItems":1,"const":[1,2]}`, `[1]`, false},
		{`{"type":"array","items":{"type":"integer"},"maxItems":-1}`, `[]`, false},
		{`{"type":"array","items":{"type":"integer"},"maxItems":1.5}`, `[]`, false},
		{`{"type":"array","items":{"type":"integer"},"maxItems":"1"}`, `[]`, false},
		{`{"type":"array","items":{"type":"integer"},"maxItems":9223372036854775808}`, `[]`, false},
		{`{"type":"integer","maxItems":1}`, `1`, false},
		{`{"type":"array","items":{"type":"integer"},"minItems":1}`, `[]`, false},
		{`{"type":"array","items":{"type":"integer"},"uniqueItems":true}`, `[]`, false},
		{`{"type":"array","prefixItems":[{"type":"integer"}]}`, `[]`, false},
	} {
		t.Run(tc.schema+tc.value, func(t *testing.T) {
			var s map[string]any
			var v any
			if err := openAPIDecode([]byte(tc.schema), &s); err != nil {
				t.Fatal(err)
			}
			if err := openAPIDecode([]byte(tc.value), &v); err != nil {
				t.Fatal(err)
			}
			err := openAPICheckSchema(s)
			if err == nil {
				err = openAPIValidateValue(s, v)
			}
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
}
func TestOpenAPIA3CoverageAndMutations(t *testing.T) {
	doc, inv := openAPILoad(t)
	count := 0
	for _, v := range inv.Normal {
		if openAPIA3Operation(v.Method + " " + openAPIPath(v.Path)) {
			count++
			if v.Coverage != "covered" || v.Authentication != "jwt" {
				t.Fatal("module coverage/auth demoted")
			}
		}
	}
	if count != 11 || len(openAPIA3Routes) != 11 {
		t.Fatalf("whole module=%d", count)
	}
	for _, key := range openAPIA3Routes {
		t.Run(key, func(t *testing.T) {
			parts := strings.SplitN(key, " ", 2)
			method, path := strings.ToLower(parts[0]), parts[1]
			op, ok := doc.Paths[path][method]
			if !ok {
				t.Fatal("missing operation")
			}
			f := openAPIA3NewFixture(t)
			r := f.router(t)
			req := httptest.NewRequest(parts[0], openAPIA2URL(path, "101"), strings.NewReader(`{"api_key_ids":[101]}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("fixture status=%d", rec.Code)
			}
			var original map[string]any
			if err := openAPIDecode(rec.Body.Bytes(), &original); err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []string{"missing required", "unexpected admin property", "wrong data type"} {
				t.Run(mutation, func(t *testing.T) {
					var b map[string]any
					if err := openAPIDecode(rec.Body.Bytes(), &b); err != nil {
						t.Fatal(err)
					}
					data := openAPIA1As[map[string]any](t, b["data"])
					switch mutation {
					case "missing required":
						delete(b, "code")
					case "unexpected admin property":
						data["account_cost"] = json.Number("99")
					case "wrong data type":
						b["data"] = "bad"
					}
					raw, err := json.Marshal(b)
					if err != nil {
						t.Fatal(err)
					}
					bad := httptest.NewRecorder()
					bad.Header().Set("Content-Type", "application/json")
					_, _ = bad.Write(raw)
					if err := openAPIValidateResponse(op, bad); err == nil {
						t.Fatal("mutated response accepted")
					}
				})
			}
		})
	}
}
