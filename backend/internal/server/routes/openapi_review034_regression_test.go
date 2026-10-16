package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIReview034Strict(t *testing.T) {
	doc, inv := openAPILoad(t)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"unknown_Headers_spelling", `"headers":`, `"Headers":`},
		{"unknown_Required_spelling", `"required":true`, `"Required":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := strings.ReplaceAll(string(raw), tc.from, tc.to)
			if changed == string(raw) {
				t.Fatal("mutation did not apply")
			}
			var candidate openAPIDocument
			err := openAPIDecode([]byte(changed), &candidate)
			if err == nil {
				err = openAPIValidateDocument(candidate, inv.Normal)
			}
			if err == nil {
				t.Fatalf("unknown OAS member %s accepted by decode + complete document validation", tc.to)
			}
		})
	}
	t.Run("duplicate_items_hides_unsupported_schema", func(t *testing.T) {
		var schema map[string]any
		err := openAPIDecode([]byte(`{"type":"array","items":{"type":"string","pattern":"unsupported"},"items":{"type":"string"}}`), &schema)
		if err == nil {
			err = openAPICheckSchema(schema)
		}
		if err == nil {
			t.Fatal("duplicate items silently erased an unsupported schema keyword")
		}
	})
	t.Run("malformed_UTF8_response", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		fmt.Fprint(rec, "{\"code\":0,\"message\":\"success\",\"data\":{\"version\":\"\xff\"}}")
		if err := openAPIValidateResponse(doc.Paths[openAPIA1Version]["get"], rec); err == nil {
			t.Fatal("invalid UTF-8 response accepted after lossy replacement")
		}
	})
	t.Run("signed_RetryAfter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		rec.Header().Set("Retry-After", "+2")
		rec.WriteHeader(429)
		fmt.Fprint(rec, `{"code":"RATE_LIMITED","message":"limited"}`)
		if err := openAPIValidateResponse(doc.Paths[openAPIA1Version]["get"], rec); err == nil {
			t.Fatal("Retry-After +2 is accepted although emitter uses decimal digits only")
		}
	})
}
