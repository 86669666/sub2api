package routes_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOpenAPIExactResponseNumbers(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, tc := range []struct{ name, number, want string }{
		{"underflow", "1e-400", "expected integer"},
		{"negative underflow", "-1e-400", "expected integer"},
		{"rounds to one", "0.99999999999999999", "expected integer"},
		{"rounds down to one", "1.00000000000000001", "expected integer"},
		{"large rounded fraction", "9007199254740992.1", "expected integer"},
		{"nonzero integer", "1", "constant mismatch"},
		{"exponent beyond profile", "0e4097", "unsupported A0 number exponent"},
		{"negative exponent beyond profile", "0e-4097", "unsupported A0 number exponent"},
		{"huge exponent", "0e999999999999999999999", "unsupported A0 number exponent"},
		{"mantissa beyond profile", strings.Repeat("1", 1025), "unsupported A0 number encoding or length"},
		{"zero", "0", ""}, {"negative zero", "-0", ""},
		{"decimal zero", "0.000", ""}, {"exponent zero", "0e-400", ""},
		{"signed exponent zero", "-0.00E+400", ""},
		{"positive exponent boundary", "0e4096", ""},
		{"negative exponent boundary", "0e-4096", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(rec, `{"code":%s,"data":{"needs_setup":false,"step":"completed"}}`, tc.number)
			err := openAPIValidateResponse(doc.Paths["/setup/status"]["get"], rec)
			if tc.want != "" {
				openAPIReject(t, err, tc.want)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenAPIExactSchemaConstants(t *testing.T) {
	for _, tc := range []struct{ name, constant, response, want string }{
		{"underflow schema", "1e-400", "0", "invalid const"},
		{"rounded fraction schema", "1.00000000000000001", "1", "invalid const"},
		{"large rounded fraction schema", "9007199254740992.1", "9007199254740992", "invalid const"},
		{"schema exponent limit", "0e4097", "0", "invalid const"},
		{"schema mantissa limit", strings.Repeat("1", 1025), "0", "invalid const"},
		{"adjacent large integers differ", "9007199254740993", "9007199254740992", "constant mismatch"},
		{"large integer exact", "9007199254740993", "9007199254740993", ""},
		{"equivalent large integer", "9007199254740993", "90071992547409930e-1", ""},
		{"equivalent one", "1.0", "10e-1", ""},
		{"equivalent zero schema", "0e-400", "-0.00e400", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var schema map[string]any
			if err := openAPIDecode([]byte(fmt.Sprintf(`{"type":"integer","const":%s}`, tc.constant)), &schema); err != nil {
				t.Fatal(err)
			}
			err := openAPICheckSchema(schema)
			if err == nil {
				var value any
				if err = openAPIDecode([]byte(tc.response), &value); err != nil {
					t.Fatal(err)
				}
				err = openAPIValidateValue(schema, value)
			}
			if tc.want != "" {
				openAPIReject(t, err, tc.want)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	// Constants on closed objects use the same exact comparison recursively.
	var schema map[string]any
	if err := openAPIDecode([]byte(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"],"additionalProperties":false,"const":{"n":9007199254740993}}`), &schema); err != nil {
		t.Fatal(err)
	}
	if err := openAPICheckSchema(schema); err != nil {
		t.Fatal(err)
	}
	var value any
	if err := openAPIDecode([]byte(`{"n":90071992547409930e-1}`), &value); err != nil {
		t.Fatal(err)
	}
	if err := openAPIValidateValue(schema, value); err != nil {
		t.Fatal(err)
	}
	if err := openAPIDecode([]byte(`{"n":9007199254740992}`), &value); err != nil {
		t.Fatal(err)
	}
	openAPIReject(t, openAPIValidateValue(schema, value), "constant mismatch")
	// Refuse a caller that bypassed the exact decoder; never accept a pre-rounded float.
	openAPIReject(t, openAPIValidateValue(map[string]any{"type": "integer"}, float64(0)), "exact JSON number")
}

func TestOpenAPIRegisteredAuthChainDrift(t *testing.T) {
	observe := func(chain ...gin.HandlerFunc) openAPIRoute {
		t.Helper()
		r, observed := openAPIRouter(t, false, false)
		r.GET("/openapi-auth-chain-probe", append(chain, func(c *gin.Context) { t.Fatal("probe reached application handler") })...)
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/openapi-auth-chain-probe", nil))
		entry, ok := observed["GET /openapi-auth-chain-probe"]
		if !ok {
			t.Fatal("missing registered probe")
		}
		return entry
	}
	admin := observe(openAPIAdmin)
	compound := observe(openAPIJWT, openAPIAdmin)
	reverse := observe(openAPIAdmin, openAPIJWT)
	duplicate := observe(openAPIAdmin, openAPIAdmin)
	stepUp := observe(openAPIAdmin, openAPIStepUp)
	if compound.Authentication != "jwt -> admin-jwt-or-key" || reverse.Authentication != "admin-jwt-or-key -> jwt" || duplicate.Authentication != "admin-jwt-or-key -> admin-jwt-or-key" {
		t.Fatal("recognized auth chain lost order or duplicates")
	}
	for _, tc := range []struct {
		name          string
		before, after openAPIRoute
	}{
		{"add preceding auth", admin, compound},
		{"remove preceding auth", compound, admin},
		{"reorder auth", compound, reverse},
		{"duplicate auth", admin, duplicate},
		{"add step up", admin, stepUp},
		{"remove step up", stepUp, admin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accepted := []openAPICoverage{{openAPIRoute: tc.before, Coverage: "uncovered", Owner: "api-contract", Reason: "registered auth chain fixture"}}
			openAPIReject(t, openAPICompare([]openAPIRoute{tc.after}, accepted), "authentication/step-up drift")
			if err := openAPICompare([]openAPIRoute{tc.before}, accepted); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Recording a chain does not claim its composed OpenAPI security is reviewed.
	_, err := openAPIExpectedSecurity(compound.Authentication)
	openAPIReject(t, err, "has not been reviewed")
}
