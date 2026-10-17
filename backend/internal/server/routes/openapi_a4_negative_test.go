package routes_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIA4CoverageAndMutations(t *testing.T) {
	doc, inv := openAPILoad(t)
	count := 0
	for _, v := range inv.Normal {
		if strings.HasPrefix(v.Path, "/api/v1/subscriptions") {
			count++
			if !openAPIA4Operation(v.Method+" "+openAPIPath(v.Path)) || v.Coverage != "covered" || v.Authentication != "jwt" {
				t.Fatal("module registration/coverage/auth drift")
			}
		}
	}
	if count != 4 || len(openAPIA4Routes) != 4 {
		t.Fatalf("whole module=%d want=4", count)
	}
	for _, key := range openAPIA4Routes {
		t.Run(key, func(t *testing.T) {
			path := strings.TrimPrefix(key, "GET ")
			op, ok := doc.Paths[path]["get"]
			if !ok {
				t.Fatal("missing operation")
			}
			if op.RequestBody != nil || len(op.Parameters) != 1 || op.Parameters[0].Name != "Accept-Language" || op.Parameters[0].In != "header" || op.Parameters[0].Required {
				t.Fatal("invented input contract")
			}
			f := openAPIA4NewFixture(t)
			r := f.router(t)
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("fixture status=%d", rec.Code)
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatal(err)
			}
			mutations := []string{"missing envelope code", "wrong data type", "null data", "unknown item field", "missing item id", "fractional id", "null group name or relation"}
			if strings.HasSuffix(path, "/progress") {
				mutations = append(mutations, "missing progress", "null progress", "unknown progress field", "missing progress id", "fractional days", "null window", "missing window field", "wrong window number", "negative reset seconds", "percentage over clamp", "negative remaining", "bad reset date")
			}
			if strings.HasSuffix(path, "/summary") {
				mutations = append(mutations, "missing total", "fractional count", "null summary array", "null optional usage", "wrong expiry type")
			}
			for _, mutation := range mutations {
				t.Run(mutation, func(t *testing.T) {
					var b map[string]any
					if err := openAPIDecode(rec.Body.Bytes(), &b); err != nil {
						t.Fatal(err)
					}
					item := openAPIA1As[map[string]any](t, openAPIA4Items(t, path, b["data"])[0])
					sub := openAPIA4Sub(t, path, item)
					switch mutation {
					case "missing envelope code":
						delete(b, "code")
					case "wrong data type":
						b["data"] = "bad"
					case "null data":
						b["data"] = nil
					case "unknown item field":
						sub["assigned_by"] = json.Number("99")
					case "missing item id":
						delete(sub, "id")
					case "fractional id":
						sub["id"] = json.Number("1.5")
					case "null group name or relation":
						if strings.HasSuffix(path, "/summary") {
							sub["group_name"] = nil
						} else {
							sub["group"] = nil
						}
					case "missing progress":
						delete(item, "progress")
					case "null progress":
						item["progress"] = nil
					case "missing total":
						delete(openAPIA1As[map[string]any](t, b["data"]), "total_used_usd")
					case "fractional count":
						openAPIA1As[map[string]any](t, b["data"])["active_count"] = json.Number("1.5")
					case "null summary array":
						openAPIA1As[map[string]any](t, b["data"])["subscriptions"] = nil
					case "null optional usage":
						sub["daily_used_usd"] = nil
					case "wrong expiry type":
						sub["expires_at"] = false
					default:
						p := openAPIA1As[map[string]any](t, item["progress"])
						switch mutation {
						case "unknown progress field":
							p["days_remaining"] = json.Number("10")
						case "missing progress id":
							delete(p, "id")
						case "fractional days":
							p["expires_in_days"] = json.Number("1.5")
						case "null window":
							p["daily"] = nil
						default:
							w := openAPIA1As[map[string]any](t, p["daily"])
							switch mutation {
							case "missing window field":
								delete(w, "used_usd")
							case "wrong window number":
								w["used_usd"] = "2.5"
							case "negative reset seconds":
								w["resets_in_seconds"] = json.Number("-1")
							case "percentage over clamp":
								w["percentage"] = json.Number("101")
							case "negative remaining":
								w["remaining_usd"] = json.Number("-1")
							case "bad reset date":
								w["resets_at"] = "tomorrow"
							}
						}
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
func TestOpenAPIA4ProgressValuesAndUnregisteredHelper(t *testing.T) {
	doc, _ := openAPILoad(t)
	f := openAPIA4NewFixture(t)
	r := f.router(t)
	for _, path := range []string{"/api/v1/subscriptions/progress", "/api/v1/subscriptions/201/progress"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("GET", path, nil)
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if strings.Contains(path, "/201/") {
				if rec.Code != 404 {
					t.Fatalf("unregistered helper status=%d want=404", rec.Code)
				}
				if _, ok := doc.Paths["/api/v1/subscriptions/{id}/progress"]; ok {
					t.Fatal("invented route counted")
				}
				return
			}
			if err := openAPIValidateResponse(doc.Paths[path]["get"], rec); err != nil {
				t.Fatal(err)
			}
			var b map[string]any
			if err := openAPIDecode(rec.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			item := openAPIA1As[map[string]any](t, openAPIA4Items(t, path, b["data"])[0])
			p := openAPIA1As[map[string]any](t, item["progress"])
			for _, v := range []struct{ name, used, limit, remaining string }{{"daily", "2.5", "10", "7.5"}, {"weekly", "5", "20", "15"}, {"monthly", "10", "40", "30"}} {
				w := openAPIA1As[map[string]any](t, p[v.name])
				if w["used_usd"] != json.Number(v.used) || w["limit_usd"] != json.Number(v.limit) || w["remaining_usd"] != json.Number(v.remaining) || w["percentage"] != json.Number("25") {
					t.Fatalf("progress math drift: %s", v.name)
				}
			}
		})
	}
}
