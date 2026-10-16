package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestOpenAPIA1FiniteProfile(t *testing.T) {
	for _, tc := range []struct{ name, schema, value, want string }{
		{"array without items", `{"type":"array"}`, `[]`, "array requires"},
		{"boolean items rejected", `{"type":"array","items":true}`, `[]`, "array requires"},
		{"tuple items rejected", `{"type":"array","items":[{"type":"string"}]}`, `[]`, "array requires"},
		{"empty items rejected", `{"type":"array","items":{}}`, `[]`, "empty schema"},
		{"unknown keyword in items", `{"type":"array","items":{"type":"string","pattern":"x"}}`, `[]`, "unsupported A0 schema keyword"},
		{"items on scalar", `{"type":"string","items":{"type":"string"}}`, `"x"`, "array keyword"},
		{"object keyword on array", `{"type":"array","items":{"type":"string"},"required":[]}`, `[]`, "object keyword"},
		{"nullable missing items", `{"type":["array","null"]}`, `null`, "array requires"},
		{"arbitrary union rejected", `{"type":["string","null"]}`, `null`, "unsupported type union"},
		{"duplicate union rejected", `{"type":["array","array"]}`, `[]`, "unsupported type union"},
		{"unknown union type rejected", `{"type":["array",{}],"items":{"type":"string"}}`, `[]`, "unsupported type union"},
		{"nullable keyword rejected", `{"type":"array","items":{"type":"string"},"nullable":true}`, `null`, "unsupported A0 schema keyword"},
		{"invalid description", `{"type":"string","description":1}`, `"x"`, "description must"},
		{"nonnullable rejects null", `{"type":"array","items":{"type":"string"}}`, `null`, "expected array"},
		{"nullable null", `{"type":["array","null"],"items":{"type":"string"}}`, `null`, ""},
		{"reverse nullable order", `{"type":["null","array"],"items":{"type":"integer"}}`, `[1,2]`, ""},
		{"nullable array element still checked", `{"type":["array","null"],"items":{"type":"integer"}}`, `[1,null]`, "exact JSON number"},
		{"array element wrong type", `{"type":"array","items":{"type":"string"}}`, `["ok",1]`, "item 1"},
		{"empty valid array", `{"type":"array","items":{"type":"string"}}`, `[]`, ""},
		{"fractional number", `{"type":"number"}`, `0.125`, ""},
		{"exact tiny number", `{"type":"number","const":1e-400}`, `10e-401`, ""},
		{"number string rejected", `{"type":"number"}`, `"0.125"`, "exact JSON number"},
		{"number bool rejected", `{"type":"number"}`, `true`, "exact JSON number"},
		{"number null rejected", `{"type":"number"}`, `null`, "exact JSON number"},
		{"number exponent bound", `{"type":"number"}`, `0e4097`, "unsupported A0 number exponent"},
		{"number const bound", `{"type":"number","const":0e4097}`, `0`, "invalid const"},
		{"array numeric const exact", `{"type":"array","items":{"type":"number"},"const":[9007199254740993,0.125]}`, `[90071992547409930e-1,125e-3]`, ""},
		{"array numeric const differs", `{"type":"array","items":{"type":"number"},"const":[9007199254740993]}`, `[9007199254740992]`, "constant mismatch"},
		{"nullable const rejected null", `{"type":["array","null"],"items":{"type":"string"},"const":[]}`, `null`, "constant mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema map[string]any
			var v any
			if err := openAPIDecode([]byte(tc.schema), &schema); err != nil {
				t.Fatal(err)
			}
			if err := openAPIDecode([]byte(tc.value), &v); err != nil {
				t.Fatal(err)
			}
			err := openAPICheckSchema(schema)
			if err == nil {
				err = openAPIValidateValue(schema, v)
			}
			if tc.want != "" {
				openAPIReject(t, err, tc.want)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	openAPIReject(t, openAPIValidateValue(map[string]any{"type": "number"}, float64(0.125)), "exact JSON number")
}
func TestOpenAPIA1CoverageCannotBeDemoted(t *testing.T) {
	doc, inv := openAPILoad(t)
	for _, path := range []string{openAPIA1Public, openAPIA1Version} {
		op, ok := doc.Paths[path]["get"]
		if !ok || op.OperationID == "" {
			t.Fatalf("required A1 operation absent: %s", path)
		}
		found := false
		for _, r := range inv.Normal {
			if r.Method == "GET" && r.Path == path {
				found = r.Coverage == "covered"
			}
		}
		if !found {
			t.Fatalf("A1 coverage demoted: %s", path)
		}
	}
}
func TestOpenAPIA1ResponseMutations(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths[openAPIA1Public]["get"]
	f := openAPIA1NewFixture(t)
	f.settings.values["custom_menu_items"] = `[{"id":"menu"}]`
	f.settings.values["custom_endpoints"] = `[{"name":"fixture"}]`
	rec := httptest.NewRecorder()
	f.router(t).ServeHTTP(rec, httptest.NewRequest("GET", openAPIA1Public, nil))
	if rec.Code != 200 {
		t.Fatalf("fixture failed: %s", rec.Body.String())
	}
	var body map[string]any
	if err := openAPIDecode(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	validate := func(t *testing.T, b map[string]any, want string) {
		t.Helper()
		r := httptest.NewRecorder()
		r.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(r).Encode(b); err != nil {
			t.Fatal(err)
		}
		openAPIReject(t, openAPIValidateResponse(op, r), want)
	}
	for key := range openAPIA1As[map[string]any](t, body["data"]) {
		t.Run("required "+key, func(t *testing.T) {
			b := openAPIClone(t, body)
			delete(openAPIA1As[map[string]any](t, b["data"]), key)
			validate(t, b, "missing required")
		})
	}
	for _, tc := range []struct {
		name, want string
		mutate     func(map[string]any)
	}{
		{"secret field injection", "unexpected property", func(d map[string]any) { d["admin_api_key"] = "synthetic" }},
		{"nested endpoint extra", "unexpected property", func(d map[string]any) {
			openAPIA1As[map[string]any](t, openAPIA1As[[]any](t, d["custom_endpoints"])[0])["secret"] = "synthetic"
		}},
		{"nested endpoint field missing", "missing required", func(d map[string]any) {
			delete(openAPIA1As[map[string]any](t, openAPIA1As[[]any](t, d["custom_endpoints"])[0]), "description")
		}},
		{"nested endpoint null", "expected object", func(d map[string]any) { d["custom_endpoints"] = []any{nil} }},
		{"wrong optional menu type", "expected boolean", func(d map[string]any) {
			openAPIA1As[map[string]any](t, openAPIA1As[[]any](t, d["custom_menu_items"])[0])["hide_open_button"] = "false"
		}},
		{"wrong scalar array item", "expected integer", func(d map[string]any) { d["table_page_size_options"] = []any{json.Number("1.5")} }},
		{"required nonnull array", "expected array", func(d map[string]any) { d["custom_menu_items"] = nil }},
		{"fractional integer", "expected integer", func(d map[string]any) { d["table_default_page_size"] = json.Number("0.125") }},
		{"wrong number type", "exact JSON number", func(d map[string]any) { d["balance_low_notify_threshold"] = "0.125" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := openAPIClone(t, body)
			tc.mutate(openAPIA1As[map[string]any](t, b["data"]))
			validate(t, b, tc.want)
		})
	}
	t.Run("retry header required", func(t *testing.T) {
		r := httptest.NewRecorder()
		r.WriteHeader(429)
		r.Header().Set("Content-Type", "application/json")
		fmt.Fprint(r, `{"code":"RATE_LIMITED","message":"limited"}`)
		openAPIReject(t, openAPIValidateResponse(op, r), "missing response header")
		r.Header().Set("Retry-After", "0")
		openAPIReject(t, openAPIValidateResponse(op, r), "invalid Retry-After")
	})
}

func TestOpenAPIA1HeaderProfileRejections(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*openAPIDocument)
	}{
		{"unknown header", func(d *openAPIDocument) {
			r := d.Paths[openAPIA1Public]["get"].Responses["429"]
			r.Headers["X-Unsupported"] = r.Headers["Retry-After"]
		}},
		{"optional retry header", func(d *openAPIDocument) {
			r := d.Paths[openAPIA1Public]["get"].Responses["429"]
			h := r.Headers["Retry-After"]
			h.Required = false
			r.Headers["Retry-After"] = h
		}},
		{"empty header schema", func(d *openAPIDocument) {
			r := d.Paths[openAPIA1Public]["get"].Responses["429"]
			h := r.Headers["Retry-After"]
			h.Schema = map[string]any{}
			r.Headers["Retry-After"] = h
		}},
		{"extra header schema keyword", func(d *openAPIDocument) {
			d.Paths[openAPIA1Public]["get"].Responses["429"].Headers["Retry-After"].Schema["pattern"] = ".*"
		}},
		{"header outside supported status", func(d *openAPIDocument) {
			op := d.Paths[openAPIA1Public]["get"]
			r := op.Responses["200"]
			r.Headers = op.Responses["429"].Headers
			op.Responses["200"] = r
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, inv := openAPILoad(t)
			tc.change(&doc)
			openAPIReject(t, openAPIValidateDocument(doc, inv.Normal), "unsupported response header contract")
		})
	}
}
