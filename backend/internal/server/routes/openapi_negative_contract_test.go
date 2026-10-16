package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func openAPIReject(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("wanted rejection containing %q, got %v", contains, err)
	}
	t.Logf("rejected as expected: %s", err)
}
func openAPILoad(t *testing.T) (openAPIDocument, openAPIInventory) {
	t.Helper()
	var doc openAPIDocument
	var inventory openAPIInventory
	openAPIRead(t, "openapi.json", &doc)
	openAPIRead(t, "route-inventory.json", &inventory)
	return doc, inventory
}
func openAPIClone[T any](t *testing.T, value T) T {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var copy T
	if err = openAPIDecode(data, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}
func TestOpenAPIDriftNegativeCases(t *testing.T) {
	// Genuine Gin registration variants exercise the SAME bidirectional comparator
	// used for the production router, rather than string/regex route extraction.
	register := func(includeOld, addNew, changeMethod bool) []openAPIRoute {
		r := gin.New()
		noop := func(c *gin.Context) { c.Status(200) }
		if includeOld {
			if changeMethod {
				r.POST("/old", noop)
			} else {
				r.GET("/old", noop)
			}
		}
		if addNew {
			r.GET("/new", noop)
		}
		var actual []openAPIRoute
		for _, entry := range r.Routes() {
			actual = append(actual, openAPIRoute{Method: entry.Method, Path: entry.Path, Authentication: "public"})
		}
		return actual
	}
	accepted := []openAPICoverage{{openAPIRoute: openAPIRoute{Method: "GET", Path: "/old", Authentication: "public"}, Coverage: "uncovered", Owner: "api-contract", Reason: "explicit fixture disposition"}}
	for _, tc := range []struct {
		name             string
		old, new, method bool
		want             string
	}{
		{"addition", true, true, false, "unaccepted route added"},
		{"removal", false, false, false, "accepted routes removed"},
		{"method change", true, false, true, "unaccepted route added"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			openAPIReject(t, openAPICompare(register(tc.old, tc.new, tc.method), accepted), tc.want)
		})
	}
	t.Run("positive control", func(t *testing.T) {
		if err := openAPICompare(register(true, false, false), accepted); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("authentication changed", func(t *testing.T) {
		actual := register(true, false, false)
		actual[0].Authentication = "jwt"
		openAPIReject(t, openAPICompare(actual, accepted), "authentication/step-up drift")
	})
	t.Run("step-up removed", func(t *testing.T) {
		copy := append([]openAPICoverage(nil), accepted...)
		copy[0].StepUp = true
		openAPIReject(t, openAPICompare(register(true, false, false), copy), "authentication/step-up drift")
	})
	t.Run("owner missing", func(t *testing.T) {
		copy := append([]openAPICoverage(nil), accepted...)
		copy[0].Owner = ""
		openAPIReject(t, openAPICompare(register(true, false, false), copy), "accountable coverage")
	})
	t.Run("duplicate inventory", func(t *testing.T) {
		openAPIReject(t, openAPICompare(register(true, false, false), append(accepted, accepted[0])), "duplicate accepted")
	})
}
func TestOpenAPIAuthenticationAnchors(t *testing.T) {
	actual := openAPICollect(t, false)
	byKey := map[string]openAPIRoute{}
	for _, entry := range actual {
		byKey[openAPIKeyOf(entry)] = entry
	}
	anchors := map[string]string{
		"GET /health":                         "public",
		"GET /api/v1/auth/me":                 "jwt",
		"GET /api/v1/admin/users":             "admin-jwt-or-key",
		"GET /api/v1/pages":                   "admin-jwt-or-key",
		"GET /api/v1/model-plaza":             "optional-jwt",
		"GET /v1/models":                      "gateway-key",
		"GET /v1beta/models":                  "google-gateway-key",
		"POST /api/v1/payment/webhook/stripe": "handler-policy-unreviewed",
	}
	for key, want := range anchors {
		if got := byKey[key].Authentication; got != want {
			t.Errorf("%s expected %s got %s", key, want, got)
		}
	}
	if !byKey["GET /api/v1/admin/accounts/data"].StepUp {
		t.Error("account export must retain the step-up seam")
	}
}
func TestOpenAPIDocumentNegativeCases(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*openAPIDocument, *openAPIInventory)
	}{
		{"wrong dialect", "metadata", func(d *openAPIDocument, _ *openAPIInventory) { d.OpenAPI = "3.0.3" }},
		{"duplicate operation ID", "operationId", func(d *openAPIDocument, _ *openAPIInventory) {
			op := d.Paths["/setup/status"]["get"]
			op.OperationID = d.Paths["/health"]["get"].OperationID
			d.Paths["/setup/status"]["get"] = op
		}},
		{"missing covered operation", "covered route missing", func(d *openAPIDocument, _ *openAPIInventory) { delete(d.Paths, "/health") }},
		{"orphan documented operation", "not registered", func(d *openAPIDocument, _ *openAPIInventory) {
			d.Paths["/orphan"] = d.Paths["/health"]
			delete(d.Paths, "/health")
		}},
		{"public route falsely protected", "authentication mislabel", func(d *openAPIDocument, _ *openAPIInventory) {
			op := d.Paths["/health"]["get"]
			s := []map[string][]string{{"BearerAuth": {}}}
			op.Security = &s
			d.Paths["/health"]["get"] = op
		}},
		{"admin route falsely public", "authentication mislabel", func(d *openAPIDocument, i *openAPIInventory) {
			op := d.Paths["/health"]["get"]
			op.OperationID = "listAdminUsers"
			d.Paths["/api/v1/admin/users"] = map[string]openAPIOperation{"get": op}
			for n := range i.Normal {
				if openAPIKeyOf(i.Normal[n].openAPIRoute) == "GET /api/v1/admin/users" {
					i.Normal[n].Coverage = "covered"
				}
			}
		}},
		{"missing explicit security", "security missing", func(d *openAPIDocument, _ *openAPIInventory) {
			op := d.Paths["/health"]["get"]
			op.Security = nil
			d.Paths["/health"]["get"] = op
		}},
		{"scheme relabeled", "scheme definition drift", func(d *openAPIDocument, _ *openAPIInventory) {
			d.Components.SecuritySchemes["AdminAPIKey"]["name"] = "unrelated-header"
		}},
		{"empty schema", "empty schema", func(d *openAPIDocument, _ *openAPIInventory) {
			s := d.Paths["/health"]["get"].Responses["200"].Content["application/json"].Schema
			for k := range s {
				delete(s, k)
			}
		}},
		{"unsupported keyword fails closed", "unsupported A0 schema keyword", func(d *openAPIDocument, _ *openAPIInventory) {
			d.Paths["/health"]["get"].Responses["200"].Content["application/json"].Schema["allOf"] = []any{}
		}},
		{"undocumented promotion", "covered route missing", func(_ *openAPIDocument, i *openAPIInventory) {
			for n := range i.Normal {
				if i.Normal[n].Coverage == "uncovered" {
					i.Normal[n].Coverage = "covered"
					break
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, i := openAPILoad(t)
			tc.mutate(&d, &i)
			openAPIReject(t, openAPIValidateDocument(d, i.Normal), tc.want)
		})
	}
}
func TestOpenAPIPathParameters(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/health"]["get"]
	doc.Paths = map[string]map[string]openAPIOperation{"/probe/{id}": {"get": op}}
	entries := []openAPICoverage{{openAPIRoute: openAPIRoute{Method: "GET", Path: "/probe/:id", Authentication: "public"}, Coverage: "covered", Owner: "api-contract", Reason: "fixture"}}
	openAPIReject(t, openAPIValidateDocument(doc, entries), "missing path parameters")
	op.Parameters = []openAPIParameter{{Name: "id", In: "path", Required: true, Schema: map[string]any{"type": "string"}}}
	doc.Paths["/probe/{id}"]["get"] = op
	if err := openAPIValidateDocument(doc, entries); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		p    openAPIParameter
	}{
		{"not required", openAPIParameter{Name: "id", In: "path", Required: false, Schema: map[string]any{"type": "string"}}},
		{"wrong name", openAPIParameter{Name: "other", In: "path", Required: true, Schema: map[string]any{"type": "string"}}},
		{"query masquerades as path", openAPIParameter{Name: "id", In: "query", Required: true, Schema: map[string]any{"type": "string"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := openAPIClone(t, doc)
			item := copy.Paths["/probe/{id}"]["get"]
			item.Parameters = []openAPIParameter{tc.p}
			copy.Paths["/probe/{id}"]["get"] = item
			openAPIReject(t, openAPIValidateDocument(copy, entries), "path parameter")
		})
	}
}
func TestOpenAPIResponseNegativeCases(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, tc := range []struct {
		name, path, method, media, body, want string
		status                                int
	}{
		{"wrong status", "/health", "get", "application/json", `{"status":"ok"}`, "undocumented HTTP status", 503},
		{"wrong media", "/health", "get", "text/html", `{"status":"ok"}`, "undocumented response media", 200},
		{"wrong const", "/health", "get", "application/json", `{"status":"bad"}`, "constant mismatch", 200},
		{"wrong type", "/health", "get", "application/json", `{"status":true}`, "expected string", 200},
		{"required missing", "/health", "get", "application/json", `{}`, "missing required", 200},
		{"extra field", "/health", "get", "application/json", `{"status":"ok","secret":"fixture"}`, "unexpected property", 200},
		{"bad JSON", "/health", "get", "application/json", `{`, "unexpected", 200},
		{"bootstrap leaks into normal status", "/setup/status", "get", "application/json", `{"code":0,"data":{"needs_setup":true,"step":"welcome"}}`, "constant mismatch", 200},
		{"telemetry grows a body", "/api/event_logging/batch", "post", "", `unexpected`, "expected empty response", 200},
		{"telemetry media drift", "/api/event_logging/batch", "post", "text/plain", ``, "expected empty response", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if tc.media != "" {
				rec.Header().Set("Content-Type", tc.media)
			}
			rec.WriteHeader(tc.status)
			if tc.body != "" {
				fmt.Fprint(rec, tc.body)
			}
			openAPIReject(t, openAPIValidateResponse(doc.Paths[tc.path][tc.method], rec), tc.want)
		})
	}
}

func TestOpenAPIActualRegistrationAddition(t *testing.T) {
	// Add a route to the production-composed router. Observation must discover it
	// through Engine.Routes(), and the committed inventory must reject it.
	r, observed := openAPIRouter(t, false, false)
	r.GET("/openapi-unaccepted-route", func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, entry := range r.Routes() {
		parts := strings.Split(entry.Path, "/")
		for n, p := range parts {
			if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
				parts[n] = "openapi-contract-probe"
			}
		}
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(entry.Method, strings.Join(parts, "/"), nil))
	}
	var actual []openAPIRoute
	for _, entry := range observed {
		actual = append(actual, entry)
	}
	var inventory openAPIInventory
	openAPIRead(t, "route-inventory.json", &inventory)
	openAPIReject(t, openAPICompare(actual, inventory.Normal), "unaccepted route added: GET /openapi-unaccepted-route")
}

func TestOpenAPIDecodeBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"second object", `{"format":1} {}`, "trailing JSON content"},
		{"second scalar", `{"format":1} null`, "trailing JSON content"},
		{"trailing junk", `{"format":1} invalid`, "trailing JSON content"},
		{"unknown typed field", `{"format":1,"unexpected":true}`, "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var inventory openAPIInventory
			openAPIReject(t, openAPIDecode([]byte(tc.input), &inventory), tc.want)
		})
	}
	var inventory openAPIInventory
	if err := openAPIDecode([]byte("{\"format\":1}\n\t "), &inventory); err != nil {
		t.Fatal(err)
	}
	if inventory.Format != 1 {
		t.Fatal("valid single JSON document was not decoded")
	}
}

func TestOpenAPIClosedSchemaProfileBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, schema, want string }{
		{"open object", `{"type":"object","properties":{"x":{"type":"string"}},"required":["x"],"additionalProperties":true}`, "objects must be closed"},
		{"implicit open object", `{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`, "objects must be closed"},
		{"nested open object", `{"type":"object","properties":{"x":{"type":"object","properties":{"y":{"type":"string"}},"required":["y"]}},"required":["x"],"additionalProperties":false}`, "objects must be closed"},
		{"unsupported array", `{"type":"array"}`, "unsupported A0 schema type"},
		{"unsupported ref", `{"type":"string","$ref":"#/components/schemas/Other"}`, "unsupported A0 schema keyword"},
		{"object keyword on scalar", `{"type":"string","additionalProperties":false}`, "object keyword on string"},
		{"unknown required property", `{"type":"object","properties":{"x":{"type":"string"}},"required":["y"],"additionalProperties":false}`, "unknown required property"},
		{"duplicate required property", `{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"],"additionalProperties":false}`, "invalid required property"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema map[string]any
			if err := openAPIDecode([]byte(tc.schema), &schema); err != nil {
				t.Fatal(err)
			}
			openAPIReject(t, openAPICheckSchema(schema), tc.want)
		})
	}
}
