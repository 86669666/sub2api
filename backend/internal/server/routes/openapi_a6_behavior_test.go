package routes_test

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func openAPIA6WriteCases(method string) []openAPIA6Case {
	cases := []openAPIA6Case{}
	add := func(name, body string, status int, invalid bool) {
		cases = append(cases, openAPIA6Case{name: name, body: body, status: status, invalidRequest: invalid})
	}
	for _, c := range []struct {
		name, body string
		status     int
		invalid    bool
	}{
		{"whitespace title", `{"title":"  ","content":"x"}`, 400, false},
		{"whitespace content", `{"title":"x","content":"  "}`, 400, false},
		{"title byte limit", `{"title":"` + strings.Repeat("界", 67) + `","content":"x"}`, 400, false},
		{"title byte boundary", `{"title":"` + strings.Repeat("界", 66) + `ab","content":"x"}`, 200, false},
		{"bad status", `{"title":"x","content":"x","status":"ACTIVE"}`, 400, false},
		{"space status", `{"title":"x","content":"x","status":" active "}`, 400, false},
		{"bad notify", `{"title":"x","content":"x","notify_mode":"loud"}`, 400, false},
		{"null pointer fields", `{"title":"x","content":"x","status":null,"notify_mode":null,"targeting":null,"starts_at":null,"ends_at":null}`, 200, false},
		{"malformed JSON", `{`, 400, true},
		{"wrong JSON shape", `[]`, 400, true},
		{"wrong field type", `{"title":2,"content":"x"}`, 400, true},
		{"decimal seconds", `{"title":"x","content":"x","starts_at":1.0}`, 400, true},
		{"exponent seconds", `{"title":"x","content":"x","ends_at":1e3}`, 400, true},
		{"overflow seconds", `{"title":"x","content":"x","ends_at":9223372036854775808}`, 400, true},
		{"JSON time overflow", `{"title":"x","content":"x","starts_at":253402300800}`, 400, false},
		{"inverted schedule", `{"title":"x","content":"x","starts_at":100,"ends_at":99}`, 400, false},
		{"equal schedule", `{"title":"x","content":"x","starts_at":100,"ends_at":100}`, 400, false},
		{"empty targeting", `{"title":"x","content":"x","targeting":{}}`, 200, false},
		{"null groups", `{"title":"x","content":"x","targeting":{"any_of":null}}`, 200, false},
		{"empty group", `{"title":"x","content":"x","targeting":{"any_of":[{}]}}`, 400, false},
		{"null group", `{"title":"x","content":"x","targeting":{"any_of":[null]}}`, 400, false},
		{"wrong targeting shape", `{"title":"x","content":"x","targeting":[]}`, 400, true},
	} {
		add(c.name, c.body, c.status, c.invalid)
	}
	for _, st := range []string{"draft", "active", "archived"} {
		for _, mode := range []string{"silent", "popup"} {
			add("enum "+st+mode, fmt.Sprintf(`{"title":"x","content":"x","status":%q,"notify_mode":%q}`, st, mode), 200, false)
		}
	}
	targetBody := func(cond string) string {
		return `{"title":"x","content":"x","targeting":{"any_of":[{"all_of":[` + cond + `]}]}}`
	}
	for _, c := range []struct {
		name, cond string
		status     int
		invalid    bool
	}{
		{"subscription", `{"type":" subscription ","operator":" in ","group_ids":[14,14]}`, 200, false},
		{"subscription no groups", `{"type":"subscription","operator":"in"}`, 400, false},
		{"subscription wrong op", `{"type":"subscription","operator":"eq","group_ids":[14]}`, 400, false},
		{"negative group", `{"type":"subscription","operator":"in","group_ids":[-1]}`, 400, false},
		{"zero group", `{"type":"subscription","operator":"in","group_ids":[0]}`, 400, false},
		{"null group ID", `{"type":"subscription","operator":"in","group_ids":[null]}`, 400, false},
		{"decimal group", `{"type":"subscription","operator":"in","group_ids":[14.0]}`, 400, true},
		{"exponent group", `{"type":"subscription","operator":"in","group_ids":[14e0]}`, 400, true},
		{"overflow group", `{"type":"subscription","operator":"in","group_ids":[9223372036854775808]}`, 400, true},
		{"balance invalid irrelevant group", `{"type":"balance","operator":"eq","group_ids":[0]}`, 400, false},
		{"balance irrelevant positive group", `{"type":"balance","operator":"eq","group_ids":[14]}`, 200, false},
		{"unknown type", `{"type":"other","operator":"eq"}`, 400, false},
		{"unknown op", `{"type":"balance","operator":"in"}`, 400, false},
		{"balance default value", `{"type":"balance","operator":"eq"}`, 200, false},
		{"balance null value", `{"type":"balance","operator":"eq","value":null}`, 200, false},
		{"balance overflow value", `{"type":"balance","operator":"eq","value":1e999}`, 400, false},
		{"unknown nested fields", `{"type":" balance ","operator":" gte ","value":-0.5,"unknown":{"role":"admin"}}`, 200, false},
	} {
		add(c.name, targetBody(c.cond), c.status, c.invalid)
	}
	for _, op := range []string{"gt", "gte", "lt", "lte", "eq"} {
		add("balance operator "+op, targetBody(fmt.Sprintf(`{"type":"balance","operator":%q,"value":-1.5}`, op)), 200, false)
	}
	cond := `{"type":"balance","operator":"eq"}`
	for _, n := range []int{50, 51} {
		status := 200
		if n == 51 {
			status = 400
		}
		add(fmt.Sprint("all_of cap ", n), `{"title":"x","content":"x","targeting":{"any_of":[{"all_of":[`+strings.TrimSuffix(strings.Repeat(cond+",", n), ",")+`]}]}}`, status, false)
		add(fmt.Sprint("any_of cap ", n), `{"title":"x","content":"x","targeting":{"any_of":[`+strings.TrimSuffix(strings.Repeat(`{"all_of":[`+cond+`]},`, n), ",")+`]}}`, status, false)
	}
	add("unknown fields ignored", `{"title":"x","content":"x","created_by":42,"updated_by":42,"targeting":{"unknown":true,"any_of":[{"unknown":true,"all_of":[{"type":"balance","operator":"eq","unknown":true}]}]}}`, 200, false)
	cases = append(cases, openAPIA6Case{name: "trim and normalization projection", status: 200, body: `{"title":"  title  ","content":"  content  ","targeting":{"any_of":[{"all_of":[{"type":" subscription ","operator":" in ","group_ids":[14,14],"value":0}]}]}}`, inspect: func(t *testing.T, _ *openAPIA6Fixture, v any) {
		m := openAPIA5Map(t, v)
		if m["title"] != "title" || m["content"] != "content" {
			t.Fatal("trim drift")
		}
		groups := openAPIA5Array(t, openAPIA5Map(t, m["targeting"])["any_of"])
		c := openAPIA5Map(t, openAPIA5Array(t, openAPIA5Map(t, groups[0])["all_of"])[0])
		if c["type"] != "subscription" || c["operator"] != "in" || len(openAPIA5Array(t, c["group_ids"])) != 2 {
			t.Fatal("normalization drift")
		}
		if _, ok := c["value"]; ok {
			t.Fatal("zero value must omit")
		}
	}})
	if method == "post" {
		add("missing required", `{}`, 400, true)
		add("root null", `null`, 400, true)
		add("empty title", `{"title":"","content":"x"}`, 400, true)
		cases = append(cases, openAPIA6Case{name: "create nonpositive ignored and defaults", status: 200, body: `{"title":"x","content":"x","status":"","notify_mode":"","starts_at":-62167219201,"ends_at":0}`, inspect: func(t *testing.T, _ *openAPIA6Fixture, v any) {
			m := openAPIA5Map(t, v)
			if m["status"] != "draft" || m["notify_mode"] != "silent" {
				t.Fatal("defaults drift")
			}
			for _, k := range []string{"starts_at", "ends_at"} {
				if _, ok := m[k]; ok {
					t.Fatal("nonpositive create schedule retained")
				}
			}
		}})
	} else {
		add("empty enum", `{"status":""}`, 400, false)
		add("empty notify", `{"notify_mode":""}`, 400, false)
		add("empty title", `{"title":""}`, 400, false)
		add("year below zero", fmt.Sprintf(`{"starts_at":%d}`, time.Date(-1, 12, 31, 0, 0, 0, 0, time.Local).Unix()), 400, false)
		add("year zero", fmt.Sprintf(`{"starts_at":%d}`, time.Date(0, 1, 2, 0, 0, 0, 0, time.Local).Unix()), 200, false)
		add("negative valid", `{"starts_at":-1}`, 200, false)
		for _, body := range []string{`{}`, `null`, `{"starts_at":null,"ends_at":null,"targeting":null,"title":null,"content":null}`} {
			cases = append(cases, openAPIA6Case{name: "preserve " + body, body: body, status: 200, change: func(f *openAPIA6Fixture) {
				openAPIA6ChangeAnn(f, func(a *service.Announcement) {
					at := time.Unix(100, 0)
					a.StartsAt = &at
					a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: "eq", Value: 10})
				})
			}, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
				a := f.anns.rows[301]
				if a.StartsAt == nil || a.StartsAt.Unix() != 100 || len(a.Targeting.AnyOf) != 1 || a.Title != "Announcement" || f.anns.writes != 1 {
					t.Fatal("null/empty preserve drift")
				}
			}})
		}
		cases = append(cases, openAPIA6Case{name: "zero clears schedule and object clears targeting", body: `{"starts_at":0,"ends_at":0,"targeting":{}}`, status: 200, change: func(f *openAPIA6Fixture) {
			openAPIA6ChangeAnn(f, func(a *service.Announcement) {
				at := time.Unix(100, 0)
				end := time.Unix(200, 0)
				a.StartsAt = &at
				a.EndsAt = &end
				a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: "eq"})
			})
		}, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
			a := f.anns.rows[301]
			if a.StartsAt != nil || a.EndsAt != nil || len(a.Targeting.AnyOf) != 0 {
				t.Fatal("clear drift")
			}
			m := openAPIA5Map(t, v)
			if _, ok := m["starts_at"]; ok {
				t.Fatal("cleared pointer not omitted")
			}
		}}, openAPIA6Case{name: "stored schedule ordering", body: `{"starts_at":300}`, status: 400, change: func(f *openAPIA6Fixture) {
			openAPIA6ChangeAnn(f, func(a *service.Announcement) { end := time.Unix(200, 0); a.EndsAt = &end })
		}})
	}
	cases = append(cases, openAPIA6Case{name: "write repository failure", status: 500, change: func(f *openAPIA6Fixture) {
		if method == "post" {
			f.anns.fail = "create"
		} else {
			f.anns.fail = "update"
		}
	}})
	return cases
}
func openAPIA6VisibilityCases(method string) []openAPIA6Case {
	cases := []openAPIA6Case{}
	for _, kind := range []string{"draft", "archived", "future", "expired", "balance", "subscription", "empty AND", "AND miss"} {
		status := 200
		if method == "post" {
			status = 404
		}
		cases = append(cases, openAPIA6Case{name: "invisible " + kind, status: status, change: func(f *openAPIA6Fixture) {
			openAPIA6ChangeAnn(f, func(a *service.Announcement) {
				switch kind {
				case "draft", "archived":
					a.Status = kind
				case "future":
					at := time.Now().Add(time.Hour)
					a.StartsAt = &at
				case "expired":
					at := time.Now().Add(-time.Hour)
					a.EndsAt = &at
				case "balance":
					a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: "gt", Value: 11})
				case "subscription":
					a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "subscription", Operator: "in", GroupIDs: []int64{99}})
				case "empty AND":
					a.Targeting = openAPIA6Target()
				case "AND miss":
					a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: "eq", Value: 10.5}, service.AnnouncementCondition{Type: "subscription", Operator: "in", GroupIDs: []int64{99}})
				}
			})
		}, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
			if f.reads.calls != 0 {
				t.Fatal("invisible announcement touched read store")
			}
			if method == "get" && len(openAPIA5Array(t, v)) != 0 {
				t.Fatal("invisible announcement leaked")
			}
		}})
	}
	cases = append(cases, openAPIA6Case{name: "OR with AND subscription hit", status: 200, change: func(f *openAPIA6Fixture) {
		openAPIA6ChangeAnn(f, func(a *service.Announcement) {
			a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: "gt", Value: 99})
			a.Targeting.AnyOf = append(a.Targeting.AnyOf, service.AnnouncementConditionGroup{AllOf: []service.AnnouncementCondition{{Type: "balance", Operator: "gte", Value: 10.5}, {Type: "subscription", Operator: "in", GroupIDs: []int64{14}}}})
			start, end := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
			a.StartsAt = &start
			a.EndsAt = &end
		})
	}})
	for _, op := range []string{"gt", "gte", "lt", "lte", "eq"} {
		for _, val := range []float64{10, 10.5, 11} {
			hit := false
			switch op {
			case "gt":
				hit = 10.5 > val
			case "gte":
				hit = 10.5 >= val
			case "lt":
				hit = 10.5 < val
			case "lte":
				hit = 10.5 <= val
			case "eq":
				hit = val == 10.5
			}
			status := 200
			if method == "post" && !hit {
				status = 404
			}
			cases = append(cases, openAPIA6Case{name: fmt.Sprintf("balance %s %v", op, val), status: status, change: func(f *openAPIA6Fixture) {
				openAPIA6ChangeAnn(f, func(a *service.Announcement) {
					a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: op, Value: val})
				})
			}, inspect: func(t *testing.T, _ *openAPIA6Fixture, v any) {
				if method == "get" {
					n := 0
					if hit {
						n = 1
					}
					if len(openAPIA5Array(t, v)) != n {
						t.Fatal("balance match drift")
					}
				}
			}})
		}
	}
	if method == "post" {
		cases = append(cases, openAPIA6Case{name: "read write failure", status: 500, change: func(f *openAPIA6Fixture) { f.reads.fail = "mark" }}, openAPIA6Case{name: "body ignored", body: `not-json user_id=42`, status: 200}, openAPIA6Case{name: "existing read preserved by repository seam", status: 200, change: func(f *openAPIA6Fixture) { f.reads.values[[2]int64{301, 41}] = time.Unix(100, 0) }, inspect: func(t *testing.T, f *openAPIA6Fixture, _ any) {
			if f.reads.attempts != 1 || f.reads.writes != 0 || f.reads.values[[2]int64{301, 41}].Unix() != 100 {
				t.Fatal("repeated mark contract drift")
			}
		}})
	} else {
		cases = append(cases, openAPIA6Case{name: "read map failure", status: 500, change: func(f *openAPIA6Fixture) { f.reads.fail = "user" }}, openAPIA6Case{name: "empty announcements", status: 200, change: func(f *openAPIA6Fixture) { f.anns.rows = map[int64]service.Announcement{} }, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
			if len(openAPIA5Array(t, v)) != 0 || f.reads.calls != 0 {
				t.Fatal("empty list read scope")
			}
		}})
		for _, q := range []string{"", "1", "true", "%20TrUe%20", "yes", "Y", "on", "false", "0", "invalid", "true&unread_only=false", "false&unread_only=true"} {
			truth := q == "1" || q == "true" || q == "%20TrUe%20" || q == "yes" || q == "Y" || q == "on" || q == "true&unread_only=false"
			cases = append(cases, openAPIA6Case{name: "unread flag " + q, query: "?unread_only=" + q, status: 200, change: func(f *openAPIA6Fixture) {
				a := f.anns.rows[301]
				a.ID = 302
				a.CreatedAt = time.Unix(1, 0)
				f.anns.rows[302] = a
				a.ID = 303
				f.anns.rows[303] = a
				f.reads.values[[2]int64{303, 41}] = time.Unix(100, 0)
				f.reads.values[[2]int64{302, 42}] = time.Unix(100, 0)
			}, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
				rows := openAPIA5Array(t, v)
				want := 3
				if truth {
					want = 2
				}
				if len(rows) != want || openAPIA5Map(t, rows[0])["id"] != json.Number("302") || openAPIA5Map(t, rows[1])["id"] != json.Number("301") {
					t.Fatal("unread filter/order/user scope drift")
				}
				if !truth && openAPIA5Map(t, rows[2])["read_at"] == nil {
					t.Fatal("read timestamp missing")
				}
				for _, row := range rows {
					m := openAPIA5Map(t, row)
					for _, field := range []string{"status", "targeting", "created_by", "updated_by", "email", "balance"} {
						if _, ok := m[field]; ok {
							t.Fatal("admin field leaked")
						}
					}
				}
				if fmt.Sprint(f.reads.ids) != "[303 302 301]" {
					t.Fatal("visible ID scope drift")
				}
			}})
		}
	}
	return cases
}
func TestOpenAPIA6TimeBoundaries(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	a := service.Announcement{Status: "active", StartsAt: &at, EndsAt: &end}
	for _, c := range []struct {
		at   time.Time
		want bool
	}{{at.Add(-time.Nanosecond), false}, {at, true}, {end.Add(-time.Nanosecond), true}, {end, false}} {
		if a.IsActiveAt(c.at) != c.want {
			t.Fatal("[start,end) boundary drift")
		}
	}
}
func TestOpenAPIA6RequestProfile(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/api/v1/admin/announcements/{id}"]["put"]
	for _, c := range []struct {
		body  string
		valid bool
	}{
		{`null`, true}, {`{}`, true}, {`{"targeting":{"extra":1,"any_of":[{"extra":1,"all_of":[{"type":"balance","operator":"eq","extra":1}]}]}}`, true},
		{`{"targeting":{"any_of":[{"all_of":[{"group_ids":[1e0]}]}]}}`, false},
		{`{"targeting":{"any_of":[{"all_of":[{"group_ids":[1.0]}]}]}}`, false},
		{`{"targeting":{"any_of":[{"all_of":[{"group_ids":["1"]}]}]}}`, false},
		{`{"targeting":{"any_of":[{"all_of":[{"group_ids":[1]}]}]}}`, true},
		{`{"targeting":{"any_of":{}}}`, false},
	} {
		t.Run(c.body, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/api/v1/admin/announcements/301", strings.NewReader(c.body))
			e := openAPIValidateRequest(op, req, map[string]string{"id": "301"}, []byte(c.body))
			if (e == nil) != c.valid {
				t.Fatalf("valid=%v err=%v", c.valid, e)
			}
		})
	}
	schema := op.RequestBody.Content["application/json"].Schema
	if e := openAPICheckSchema(schema); e == nil {
		t.Fatal("open request schema accepted as response")
	}
	raw, _ := json.Marshal(schema)
	var changed map[string]any
	if e := openAPIDecode(raw, &changed); e != nil {
		t.Fatal(e)
	}
	changed["unexpectedKeyword"] = true
	if e := openAPICheckRequestSchema(changed); e == nil {
		t.Fatal("unknown schema keyword accepted")
	}
}

func TestOpenAPIA6Lifecycle(t *testing.T) {
	doc, _ := openAPILoad(t)
	f := openAPIA6NewFixture(t, true)
	r := f.router(t)
	call := func(method, path, id, body string) map[string]any {
		t.Helper()
		op := doc.Paths[path][strings.ToLower(method)]
		req := httptest.NewRequest(method, openAPIA2URL(path, id), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s status=%d body=%s", method, rec.Code, rec.Body.String())
		}
		if e := openAPIValidateResponse(op, rec); e != nil {
			t.Fatal(e)
		}
		var out map[string]any
		if e := openAPIDecode(rec.Body.Bytes(), &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	root := "/api/v1/admin/announcements"
	created := openAPIA5Map(t, call("POST", root, "", `{"title":"Lifecycle","content":"Content","status":"active","password":"A6-REDACT-CANARY"}`)["data"])
	id := string(openAPIA1As[json.Number](t, created["id"]))
	if id != "901" {
		t.Fatal("create identity")
	}
	call("GET", root+"/{id}", id, "")
	updated := openAPIA5Map(t, call("PUT", root+"/{id}", id, `{"title":"Changed","notify_mode":"popup"}`)["data"])
	if updated["title"] != "Changed" || updated["notify_mode"] != "popup" {
		t.Fatal("update not persisted")
	}
	call("POST", "/api/v1/announcements/{id}/read", id, "")
	first := f.reads.values[[2]int64{901, 41}]
	call("POST", "/api/v1/announcements/{id}/read", id, "")
	if f.reads.attempts != 2 || f.reads.writes != 1 || !first.Equal(f.reads.values[[2]int64{901, 41}]) {
		t.Fatal("read repeat/persistence seam drift")
	}
	list := openAPIA5Array(t, call("GET", "/api/v1/announcements", "", "")["data"])
	if len(list) != 2 || openAPIA5Map(t, list[1])["read_at"] == nil {
		t.Fatal("read state not reflected")
	}
	call("DELETE", root+"/{id}", id, "")
	if _, ok := f.anns.rows[901]; ok {
		t.Fatal("delete not persisted")
	}
	f.auditService.Stop()
	if len(f.audit.logs) != 5 {
		t.Fatalf("audit count=%d", len(f.audit.logs))
	}
	for _, a := range f.audit.logs {
		if strings.Contains(a.RequestBody, "A6-REDACT-CANARY") {
			t.Fatal("audit secret not redacted")
		}
		if a.ActorUserID == nil || *a.ActorUserID != 41 {
			t.Fatal("audit actor drift")
		}
	}
}
func TestOpenAPIA6ResponseNegatives(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, key := range openAPIA6Routes {
		method, path, _ := strings.Cut(key, " ")
		t.Run(key, func(t *testing.T) {
			f := openAPIA6NewFixture(t, strings.Contains(path, "/admin/"))
			r := f.router(t)
			req := httptest.NewRequest(method, openAPIA2URL(path, "301"), strings.NewReader(`{"title":"x","content":"x"}`))
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatal(rec.Body.String())
			}
			op := doc.Paths[path][strings.ToLower(method)]
			schema := op.Responses["200"].Content["application/json"].Schema
			for _, mutation := range []string{"missing code", "wrong code", "extra envelope", "null data", "data projection"} {
				t.Run(mutation, func(t *testing.T) {
					var v map[string]any
					if e := openAPIDecode(rec.Body.Bytes(), &v); e != nil {
						t.Fatal(e)
					}
					switch mutation {
					case "missing code":
						delete(v, "code")
					case "wrong code":
						v["code"] = json.Number("1")
					case "extra envelope":
						v["secret"] = true
					case "null data":
						v["data"] = nil
					case "data projection":
						data := v["data"]
						if list, ok := data.([]any); ok {
							data = list[0]
						}
						m := openAPIA5Map(t, data)
						if list, ok := m["items"].([]any); ok {
							m = openAPIA5Map(t, list[0])
						}
						m["PRIVATE-A6-FIELD"] = true
					}
					if e := openAPIValidateValue(schema, v); e == nil {
						t.Fatal("invalid response accepted")
					}
				})
			}
		})
	}
}
