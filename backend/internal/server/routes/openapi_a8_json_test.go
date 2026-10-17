package routes_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

// This explicit exception is limited to provider body_override JSON objects.
// Other response DTOs remain closed and typed maps retain their value schema.
func openAPIA8FreeformMap(s map[string]any) bool {
	if s["x-freeform-json"] != true || s["additionalProperties"] != true {
		return false
	}
	typ := s["type"]
	valid := typ == "object"
	if ts, ok := typ.([]any); ok {
		valid = len(ts) == 2 && ts[0] == "object" && ts[1] == "null"
	}
	p, pok := s["properties"].(map[string]any)
	r, rok := s["required"].([]any)
	d, dok := s["description"].(string)
	return valid && pok && len(p) == 0 && rok && len(r) == 0 && dok && d != ""
}
func openAPIA8JSONValue(v any) error {
	switch x := v.(type) {
	case nil, bool, string:
		return nil
	case json.Number:
		_, e := openAPIExactNumber(x)
		return e
	case []any:
		for _, v := range x {
			if e := openAPIA8JSONValue(v); e != nil {
				return e
			}
		}
		return nil
	case map[string]any:
		for _, v := range x {
			if e := openAPIA8JSONValue(v); e != nil {
				return e
			}
		}
		return nil
	default:
		return fmt.Errorf("non JSON value %T", v)
	}
}
func TestOpenAPIA8FreeformJSON(t *testing.T) {
	doc, _ := openAPILoad(t)
	s := doc.Paths["/api/v1/admin/channel-monitors"]["post"].RequestBody.Content["application/json"].Schema["properties"].(map[string]any)["body_override"].(map[string]any)
	if !openAPIA8FreeformMap(s) {
		t.Fatal("missing explicit freeform schema")
	}
	for _, v := range []any{nil, true, "", json.Number("1.25"), []any{nil, map[string]any{"arbitrary": json.Number("10")}}, map[string]any{"deep": []any{false}}} {
		if e := openAPIA8JSONValue(v); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []any{json.Number("NaN"), make(chan int), []any{func() {}}, map[string]any{"bad": complex(1, 2)}} {
		if openAPIA8JSONValue(v) == nil {
			t.Fatalf("accepted %T", v)
		}
	}
	for _, key := range []string{"x-freeform-json", "type", "additionalProperties", "properties", "required", "description"} {
		c := openAPIA8Clone(s)
		delete(c, key)
		if openAPIA8FreeformMap(c) {
			t.Fatalf("accepted missing %s", key)
		}
	}
	c := openAPIA8Clone(s)
	c["type"] = "array"
	if openAPICheckSchema(c) == nil {
		t.Fatal("freeform marker escaped object scope")
	}
}
