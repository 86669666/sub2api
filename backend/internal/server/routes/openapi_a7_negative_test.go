package routes_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIA7CoverageAndNegatives(t *testing.T) {
	d, inv := openAPILoad(t)
	count := 0
	for _, r := range inv.Normal {
		if strings.HasPrefix(r.Path, "/api/v1/admin/channel-monitor-v2/") && openAPIA7Operation(r.Method+" "+r.Path) {
			count++
			if r.Coverage != "covered" || r.Authentication != "admin-jwt-or-key" {
				t.Fatal("module coverage drift")
			}
		}
	}
	if count != 8 {
		t.Fatalf("module=%d want=8", count)
	}
	for _, key := range openAPIA7Routes {
		method, path, _ := strings.Cut(key, " ")
		t.Run(key, func(t *testing.T) {
			f := openAPIA7NewFixture(t)
			r := f.router(t)
			req := httptest.NewRequest(method, path, strings.NewReader(`{"version":2}`))
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			f.auditService.Stop()
			if rec.Code != 200 {
				t.Fatal(rec.Body.String())
			}
			op := d.Paths[path][strings.ToLower(method)]
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []string{"missing code", "wrong code", "extra envelope", "wrong data", "extra data", "missing data field", "nested type"} {
				t.Run(mutation, func(t *testing.T) {
					var b map[string]any
					if err := openAPIDecode(rec.Body.Bytes(), &b); err != nil {
						t.Fatal(err)
					}
					data := openAPIA5Map(t, b["data"])
					switch mutation {
					case "missing code":
						delete(b, "code")
					case "wrong code":
						b["code"] = json.Number("1")
					case "extra envelope":
						b["secret"] = true
					case "wrong data":
						b["data"] = true
					case "extra data":
						data["secret"] = true
					case "missing data field":
						if strings.HasSuffix(path, "/config") {
							delete(data, "version")
						} else if strings.HasSuffix(path, "/snapshot") {
							delete(data, "metrics")
						} else if strings.HasSuffix(path, "/dimensions") {
							delete(data, "groups")
						} else {
							delete(data, "items")
						}
					case "nested type":
						switch {
						case strings.HasSuffix(path, "/config"):
							openAPIA5Map(t, data["health_thresholds"])["minimum_sample"] = "50"
						case strings.HasSuffix(path, "/snapshot"):
							openAPIA5Map(t, data["metrics"])["upstream_attempt_count"] = "9"
						case strings.HasSuffix(path, "/dimensions"):
							openAPIA5Map(t, openAPIA5Array(t, data["groups"])[0])["request_count"] = "100"
						case strings.HasSuffix(path, "/errors"):
							openAPIA5Map(t, openAPIA5Array(t, openAPIA5Map(t, openAPIA5Array(t, data["items"])[0])["details"])[0])["status_code"] = "502"
						case strings.HasSuffix(path, "/users"):
							openAPIA5Map(t, openAPIA5Array(t, data["items"])[0])["user_id"] = "42"
						default:
							openAPIA5Map(t, openAPIA5Map(t, openAPIA5Array(t, data["items"])[0])["metrics"])["request_count"] = "100"
						}
					}
					if err := openAPIValidateValue(op.Responses["200"].Content["application/json"].Schema, b); err == nil {
						t.Fatal("invalid response accepted")
					}
				})
			}
			for _, wrong := range []any{json.Number("429"), "OTHER_RATE_LIMIT"} {
				v := map[string]any{"code": wrong, "message": "synthetic"}
				if err := openAPIValidateValue(op.Responses["429"].Content["application/json"].Schema, v); err == nil {
					t.Fatal("wrong limiter code accepted")
				}
			}
			if _, ok := op.Responses["404"]; ok {
				t.Fatal("invented 404 in V2 config/analytics")
			}
			for _, status := range []string{"403", "429", "500"} {
				t.Run("error type "+status, func(t *testing.T) {
					v := map[string]any{"code": true, "message": "synthetic"}
					if err := openAPIValidateValue(op.Responses[status].Content["application/json"].Schema, v); err == nil {
						t.Fatal("invalid error code accepted")
					}
				})
			}
		})
	}
}
