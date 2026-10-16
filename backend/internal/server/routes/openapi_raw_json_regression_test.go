package routes_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIRawJSONRejectsAmbiguity(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"duplicate root", `{"code":999,"code":0}`, `$["code"]`},
		{"escaped duplicate", `{"code":999,"\u0063ode":0}`, `$["code"]`},
		{"nested array duplicate", `{"data":[{"nested":{"x":1,"x":2}}]}`, `$["data"][0]["nested"]["x"]`},
		{"unicode duplicate", `{"图":1,"\u56fe":2}`, `$["图"]`},
		{"duplicate const member", `{"const":{"value":0,"value":1}}`, `$["const"]["value"]`},
		{"duplicate dynamic schema property", `{"properties":{"x":{"type":"string"},"x":{"type":"number"}}}`, `$["properties"]["x"]`},
		{"invalid utf8 key", "{\"\xff\":0}", "invalid UTF-8"},
		{"invalid utf8 value", "{\"x\":\"\xc0\xaf\"}", "invalid UTF-8"},
		{"invalid utf8 nested", "[{\"x\":\"\xed\xa0\x80\"}]", "invalid UTF-8"},
		{"truncated object", `{"x":`, "$"},
		{"truncated array", `[1,`, "$"},
		{"wrong delimiter", `{"x":]}`, "$"},
		{"trailing value", `{"x":1} 2`, "trailing JSON content"},
		{"trailing junk", `[] garbage`, "trailing JSON content"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value any
			openAPIReject(t, openAPIDecode([]byte(tc.raw), &value), tc.want)
		})
	}
	// The duplicate policy applies below arbitrary object/array nesting, not just
	// at known schema paths or within the handful of immutable reviewer probes.
	raw := strings.Repeat(`{"a":[`, 40) + `{"n":0,"\u006e":1}` + strings.Repeat(`]}`, 40)
	var value any
	openAPIReject(t, openAPIDecode([]byte(raw), &value), "duplicate object member")
}

func TestOpenAPIRawJSONExactStructuralMembers(t *testing.T) {
	cases := []struct {
		name, raw, want string
		destination     func() any
	}{
		{"document", `{"OpenAPI":"3.1.1"}`, `$["OpenAPI"]`, func() any { return new(openAPIDocument) }},
		{"info", `{"info":{"Title":"x"}}`, `$["info"]["Title"]`, func() any { return new(openAPIDocument) }},
		{"components", `{"components":{"SecuritySchemes":{}}}`, `$["components"]["SecuritySchemes"]`, func() any { return new(openAPIDocument) }},
		{"operation map", `{"paths":{"/x":{"get":{"Responses":{}}}}}`, `$["paths"]["/x"]["get"]["Responses"]`, func() any { return new(openAPIDocument) }},
		{"header map", `{"headers":{"Retry-After":{"Required":true}}}`, `$["headers"]["Retry-After"]["Required"]`, func() any { return new(openAPIResponse) }},
		{"content map", `{"content":{"application/json":{"Schema":{}}}}`, `$["content"]["application/json"]["Schema"]`, func() any { return new(openAPIResponse) }},
		{"parameter slice", `{"parameters":[{"Name":"x"}]}`, `$["parameters"][0]["Name"]`, func() any { return new(openAPIOperation) }},
		{"embedded inventory", `{"normal":[{"Method":"GET"}]}`, `$["normal"][0]["Method"]`, func() any { return new(openAPIInventory) }},
		{"escaped wrong spelling", `{"\u0048eaders":{}}`, `$["Headers"]`, func() any { return new(openAPIResponse) }},
		{"duplicate header", `{"headers":{"Retry-After":{},"Retry-After":{}}}`, `$["headers"]["Retry-After"]`, func() any { return new(openAPIResponse) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { openAPIReject(t, openAPIDecode([]byte(tc.raw), tc.destination()), tc.want) })
	}
}

func TestOpenAPIRawJSONPreservesValidInput(t *testing.T) {
	t.Run("unicode and exact nested numbers", func(t *testing.T) {
		raw := `{"文本":"图像😀�","escaped":"\u56fe\u50cf\ud83d\ude00\ufffd","a":[null,{"number":9007199254740993,"small":1e-400,"fraction":0.125}],"A":1,"aCase":2}`
		var value map[string]any
		if err := openAPIDecode([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		if value["文本"] != value["escaped"] {
			t.Fatal("Unicode value changed")
		}
		items, ok := value["a"].([]any)
		if !ok || len(items) != 2 {
			t.Fatalf("a: expected two-element array, got %T: %v", value["a"], value["a"])
		}
		nested, ok := items[1].(map[string]any)
		if !ok {
			t.Fatalf("a[1]: expected object, got %T: %v", items[1], items[1])
		}
		for key, want := range map[string]string{"number": "9007199254740993", "small": "1e-400", "fraction": "0.125"} {
			n, ok := nested[key].(json.Number)
			if !ok || n.String() != want {
				t.Fatalf("%s lost original exact number: %v", key, n)
			}
		}
	})
	t.Run("dynamic properties are case sensitive data", func(t *testing.T) {
		var schema map[string]any
		raw := `{"type":"object","properties":{"Headers":{"type":"string"},"Required":{"type":"number"},"图":{"type":["array","null"],"items":{"type":"integer"}}},"required":["Headers","Required","图"],"additionalProperties":false}`
		if err := openAPIDecode([]byte(raw), &schema); err != nil {
			t.Fatal(err)
		}
		if err := openAPICheckSchema(schema); err != nil {
			t.Fatal(err)
		}
		for _, body := range []string{`{"Headers":"ok","Required":0.125,"图":null}`, `{"Headers":"ok","Required":0.125,"图":[9007199254740993,1e2]}`} {
			var value any
			if err := openAPIDecode([]byte(body), &value); err != nil {
				t.Fatal(err)
			}
			if err := openAPIValidateValue(schema, value); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("escaped exact structural member", func(t *testing.T) {
		var response openAPIResponse
		if err := openAPIDecode([]byte(`{"\u0068eaders":{"Retry-After":{"required":true,"schema":{"type":"string"}}}}`), &response); err != nil {
			t.Fatal(err)
		}
		if !response.Headers["Retry-After"].Required {
			t.Fatal("escaped member not decoded")
		}
	})
	t.Run("independent object key scopes", func(t *testing.T) {
		var value any
		if err := openAPIDecode([]byte(`[{"x":1},{"x":2}, {"x":{"x":3}}]`), &value); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("deep arrays and objects", func(t *testing.T) {
		raw := strings.Repeat(`{"a":[`, 40) + `9007199254740993` + strings.Repeat(`]}`, 40)
		var value any
		if err := openAPIDecode([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 40; i++ {
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("depth %d: expected object, got %T: %v", i, value, value)
			}
			items, ok := object["a"].([]any)
			if !ok || len(items) != 1 {
				t.Fatalf("depth %d a: expected one-element array, got %T: %v", i, object["a"], object["a"])
			}
			value = items[0]
		}
		if value != json.Number("9007199254740993") {
			t.Fatal("deep number changed")
		}
	})
	t.Run("typed document round trip", func(t *testing.T) {
		doc, inventory := openAPILoad(t)
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		var got openAPIDocument
		if err := openAPIDecode(raw, &got); err != nil {
			t.Fatal(err)
		}
		roundTrip, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		// omitempty may turn an empty slice into nil; compare the JSON contract.
		if !bytes.Equal(raw, roundTrip) {
			t.Fatal("valid document JSON changed")
		}
		if err := openAPIValidateDocument(got, inventory.Normal); err != nil {
			t.Fatal(err)
		}
	})
}

func TestOpenAPIRetryAfterDecimalSeconds(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths[openAPIA1Version]["get"]
	for _, tc := range []struct {
		name   string
		values []string
		valid  bool
	}{
		{"emitter", []string{"2"}, true}, {"positive minimum", []string{"1"}, true},
		{"bounded maximum", []string{"9223372036854775807"}, true}, {"leading zeros", []string{"002"}, true},
		{"missing", nil, false}, {"empty", []string{""}, false}, {"plus", []string{"+2"}, false},
		{"negative", []string{"-2"}, false}, {"zero", []string{"0"}, false}, {"zero padded", []string{"00"}, false},
		{"fraction", []string{"2.0"}, false}, {"exponent", []string{"2e0"}, false},
		{"leading space", []string{" 2"}, false}, {"trailing space", []string{"2 "}, false}, {"tab", []string{"\t2"}, false},
		{"comma list", []string{"2,3"}, false}, {"multiple lines", []string{"2", "3"}, false}, {"duplicate identical", []string{"2", "2"}, false},
		{"unicode digits", []string{"２"}, false}, {"overflow", []string{"9223372036854775808"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "application/json")
			for _, value := range tc.values {
				rec.Header().Add("Retry-After", value)
			}
			rec.WriteHeader(429)
			fmt.Fprint(rec, `{"code":"RATE_LIMITED","message":"limited"}`)
			err := openAPIValidateResponse(op, rec)
			if (err == nil) != tc.valid {
				t.Fatalf("values %q validity %v: %v", tc.values, tc.valid, err)
			}
		})
	}
}
