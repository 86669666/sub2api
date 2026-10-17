package routes_test

import (
	"fmt"
	"testing"
)

// oneOf branches use the same finite typed schema profile. No reference resolver,
// inference or permissive empty-schema fallback is introduced.
func openAPIA9CheckAlternatives(schema map[string]any) error {
	raw, exists := schema["oneOf"]
	if !exists {
		return nil
	}
	branches, ok := raw.([]any)
	if !ok || len(branches) < 2 {
		return fmt.Errorf("oneOf requires at least two concrete schemas")
	}
	for _, raw := range branches {
		s, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("oneOf branch must be a schema")
		}
		if e := openAPICheckSchema(s); e != nil {
			return fmt.Errorf("oneOf: %w", e)
		}
	}
	return nil
}
func openAPIA9ValidateAlternatives(schema map[string]any, value any) error {
	raw, exists := schema["oneOf"]
	if !exists {
		return nil
	}
	branches, ok := raw.([]any)
	if !ok || len(branches) < 2 {
		return fmt.Errorf("invalid oneOf")
	}
	matches := 0
	for _, raw := range branches {
		s, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid oneOf branch")
		}
		if openAPIValidateValue(s, value) == nil {
			matches++
		}
	}
	if matches != 1 {
		return fmt.Errorf("oneOf matched %d branches", matches)
	}
	return nil
}
func TestOpenAPIA9ConditionalPricing(t *testing.T) {
	d, _ := openAPILoad(t)
	op := d.Paths["/api/v1/admin/channels/model-pricing"]["get"]
	_, b := openAPIA9Run(t, "get", "/api/v1/admin/channels/model-pricing", op, openAPIA9Case{status: 200})
	s := op.Responses["200"].Content["application/json"].Schema
	for _, mutation := range []string{"remove price", "false with prices", "unknown field", "wrong price"} {
		t.Run(mutation, func(t *testing.T) {
			c := openAPIA8Clone(b)
			data := openAPIA5Map(t, c["data"])
			switch mutation {
			case "remove price":
				delete(data, "input_price")
			case "false with prices":
				data["found"] = false
			case "unknown field":
				data["credential"] = "no"
			case "wrong price":
				data["input_price"] = "0.1"
			}
			if openAPIValidateValue(s, c) == nil {
				t.Fatal("invalid conditional response accepted")
			}
		})
	}
	valid := map[string]any{"type": "boolean"}
	for _, branches := range []any{true, []any{}, []any{valid}, []any{valid, map[string]any{}}, []any{valid, true}, []any{valid, map[string]any{"type": "boolean", "unknown": true}}} {
		if openAPICheckSchema(map[string]any{"type": "boolean", "oneOf": branches}) == nil {
			t.Fatal("invalid branch declaration accepted")
		}
	}
	overlap := map[string]any{"type": "boolean", "oneOf": []any{valid, valid}}
	if openAPIValidateValue(overlap, true) == nil {
		t.Fatal("overlapping oneOf branches accepted")
	}
	noMatch := map[string]any{"type": "boolean", "oneOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "integer"}}}
	if openAPIValidateValue(noMatch, true) == nil {
		t.Fatal("no oneOf match accepted")
	}
}
