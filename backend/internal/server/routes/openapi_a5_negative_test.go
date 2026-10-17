package routes_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func openAPIA5Response(t *testing.T, path string) (openAPIOperation, []byte) {
	t.Helper()
	doc, _ := openAPILoad(t)
	op := doc.Paths[path]["get"]
	f := openAPIA5NewFixture(t, path)
	f.base.a1.settings.values["channel_monitor_show_quota"] = "true"
	r := f.router(t)
	req := httptest.NewRequest("GET", openAPIA2URL(path, "301"), nil)
	req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("fixture status=%d", rec.Code)
	}
	if err := openAPIValidateResponse(op, rec); err != nil {
		t.Fatal(err)
	}
	return op, rec.Body.Bytes()
}
func TestOpenAPIA5CoverageAndMutations(t *testing.T) {
	doc, inv := openAPILoad(t)
	count := 0
	for _, v := range inv.Normal {
		if strings.HasPrefix(v.Path, "/api/v1/channel-monitors") || strings.HasPrefix(v.Path, "/api/v1/channel-monitor-v2/") {
			count++
			if !openAPIA5Operation(v.Method+" "+openAPIPath(v.Path)) || v.Coverage != "covered" || v.Authentication != "jwt" {
				t.Fatal("module coverage/auth drift")
			}
		}
	}
	if count != 8 || len(openAPIA5Routes) != 8 {
		t.Fatalf("module=%d want=8", count)
	}
	for _, key := range openAPIA5Routes {
		path := strings.TrimPrefix(key, "GET ")
		t.Run(path, func(t *testing.T) {
			op, raw := openAPIA5Response(t, path)
			if _, ok := doc.Paths[path]["get"]; !ok {
				t.Fatal("missing operation")
			}
			if op.RequestBody != nil {
				t.Fatal("invented request body")
			}
			mutations := []string{"missing envelope", "null data", "extra data property", "wrong data type", "missing data field", "wrong nested type"}
			if stringsContainsV2(path) {
				if !strings.HasSuffix(path, "/dimensions") && !strings.HasSuffix(path, "/errors") {
					mutations = append(mutations, "volume leak", "upstream field leak", "sample count leak", "wrong latency type")
				}
				if !strings.HasSuffix(path, "/dimensions") {
					mutations = append(mutations, "bad coverage date", "missing coverage field")
				}
				if strings.HasSuffix(path, "/snapshot") {
					mutations = append(mutations, "operator identity leak", "policy group list leak", "policy model list leak")
				}
			}
			if path == "/api/v1/channel-monitors" {
				mutations = append(mutations, "quota wrong balance", "quota unknown field", "quota missing tier field", "null timeline")
			}
			for _, mutation := range mutations {
				t.Run(mutation, func(t *testing.T) {
					var b map[string]any
					if err := openAPIDecode(raw, &b); err != nil {
						t.Fatal(err)
					}
					data := openAPIA5Map(t, b["data"])
					switch mutation {
					case "missing envelope":
						delete(b, "code")
					case "null data":
						b["data"] = nil
					case "wrong data type":
						b["data"] = "bad"
					case "extra data property":
						data["api_key"] = "synthetic"
					case "missing data field":
						switch {
						case strings.HasSuffix(path, "/snapshot"):
							delete(data, "metrics")
						case strings.HasSuffix(path, "/dimensions"):
							delete(data, "groups")
						case strings.Contains(path, "{id}"):
							delete(data, "models")
						default:
							delete(data, "items")
						}
					case "wrong nested type":
						switch {
						case strings.HasSuffix(path, "/snapshot"):
							openAPIA5Map(t, data["health"])["thresholds"] = false
						case strings.HasSuffix(path, "/dimensions"):
							openAPIA5Map(t, openAPIA5Array(t, data["groups"])[0])["request_count"] = "100"
						case strings.Contains(path, "{id}"):
							openAPIA5Map(t, openAPIA5Array(t, data["models"])[0])["latest_latency_ms"] = "12"
						default:
							openAPIA5Map(t, openAPIA5Array(t, data["items"])[0])["unknown"] = true
						}
					case "bad coverage date":
						openAPIA5Map(t, data["coverage"])["requested_start"] = "yesterday"
					case "missing coverage field":
						delete(openAPIA5Map(t, data["coverage"]), "coverage_complete")
					case "operator identity leak":
						openAPIA5Map(t, data["config"])["updated_by"] = json.Number("1")
					case "policy group list leak":
						openAPIA5Map(t, data["config"])["group_ids"] = []any{json.Number("11")}
					case "policy model list leak":
						openAPIA5Map(t, openAPIA5Array(t, openAPIA5Map(t, data["config"])["platforms"])[0])["models"] = []any{"secret"}
					case "quota wrong balance", "quota unknown field", "quota missing tier field", "null timeline":
						row := openAPIA5Map(t, openAPIA5Array(t, data["items"])[0])
						q := openAPIA5Map(t, row["latest_quota"])
						switch mutation {
						case "quota wrong balance":
							q["balance"] = "bad"
						case "quota unknown field":
							q["secret"] = true
						case "quota missing tier field":
							delete(openAPIA5Map(t, openAPIA5Array(t, q["tiers"])[0]), "used_percent")
						case "null timeline":
							row["timeline"] = nil
						}
					default:
						row := data
						if !strings.HasSuffix(path, "/snapshot") {
							row = openAPIA5Map(t, openAPIA5Array(t, data["items"])[0])
						}
						m := openAPIA5Map(t, row["metrics"])
						switch mutation {
						case "volume leak":
							m["request_count"] = json.Number("1")
						case "upstream field leak":
							m["upstream_attempt_count"] = json.Number("1")
						case "sample count leak":
							openAPIA5Map(t, m["ttft"])["sample_count"] = json.Number("1")
						case "wrong latency type":
							openAPIA5Map(t, m["duration"])["p90_ms"] = "bad"
						}
					}
					changed, err := json.Marshal(b)
					if err != nil {
						t.Fatal(err)
					}
					rec := httptest.NewRecorder()
					rec.Header().Set("Content-Type", "application/json")
					_, _ = rec.Write(changed)
					if err := openAPIValidateResponse(op, rec); err == nil {
						t.Fatal("mutated response accepted")
					}
				})
			}
		})
	}
}
