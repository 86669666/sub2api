package routes_test

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var openAPIA6Routes = []string{"GET /api/v1/admin/announcements", "POST /api/v1/admin/announcements", "GET /api/v1/admin/announcements/{id}", "PUT /api/v1/admin/announcements/{id}", "DELETE /api/v1/admin/announcements/{id}", "GET /api/v1/admin/announcements/{id}/read-status", "GET /api/v1/announcements", "POST /api/v1/announcements/{id}/read"}

func openAPIA6Operation(key string) bool {
	for _, k := range openAPIA6Routes {
		if key == k {
			return true
		}
	}
	return false
}

type openAPIA6Case struct {
	name, id, query, body, auth, reason string
	headers                             map[string]string
	status                              int
	invalidRequest                      bool
	change                              func(*openAPIA6Fixture)
	inspect                             func(*testing.T, *openAPIA6Fixture, any)
}

func openAPIA6Target(conditions ...service.AnnouncementCondition) service.AnnouncementTargeting {
	return service.AnnouncementTargeting{AnyOf: []service.AnnouncementConditionGroup{{AllOf: conditions}}}
}
func openAPIA6ChangeAnn(f *openAPIA6Fixture, fn func(*service.Announcement)) {
	a := f.anns.rows[301]
	fn(&a)
	f.anns.rows[301] = a
}
func openAPIA6Cases(t *testing.T, method, path string) []openAPIA6Case {
	admin := strings.Contains(path, "/admin/")
	detail := strings.Contains(path, "{id}")
	readStatus := strings.HasSuffix(path, "read-status")
	write := admin && (method == "post" || method == "put")
	token := openAPIA1Token(t, nil, openAPIA1SigningKey)
	limit := func(f *openAPIA6Fixture) {
		f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"public_ip_rpm":1,"exempt_admin":false}`
		f.a1.counter.count = 2
	}
	cases := []openAPIA6Case{
		{name: "populated", status: 200},
		{name: "missing auth", auth: "-", status: 401},
		{name: "invalid token", auth: "Bearer invalid", status: 401},
		{name: "wrong scheme", auth: "Basic invalid", status: 401},
		{name: "expired", auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey), status: 401},
		{name: "revoked", status: 401, change: func(f *openAPIA6Fixture) { f.a1.users.user.TokenVersion++ }},
		{name: "inactive", status: 401, change: func(f *openAPIA6Fixture) { f.a1.users.user.Status = "disabled" }},
		{name: "missing user", status: 401, change: func(f *openAPIA6Fixture) { f.a1.users.lookupErr = service.ErrUserNotFound }},
		{name: "auth store failure", status: 500, change: func(f *openAPIA6Fixture) { f.a1.users.lookupErr = fmt.Errorf("PRIVATE-A6-AUTH") }},
		{name: "repository failure", status: 500, change: func(f *openAPIA6Fixture) { f.anns.fail = "all" }},
		{name: "global limiter", status: 429, change: limit},
		{name: "limiter fails open", status: 200, change: func(f *openAPIA6Fixture) { limit(f); f.a1.counter.fail = true }},
		{name: "spoofed identity ignored", status: 200, query: "?user_id=42&role=admin&created_by=42", headers: map[string]string{"X-User-ID": "42", "X-User-Role": "admin"}},
	}
	bound := openAPIA1Token(t, func(c *service.JWTClaims) {
		c.BindingHash = (&service.SessionBinding{IP: "203.0.113.9", UserAgent: "a6-bound"}).Hash()
	}, openAPIA1SigningKey)
	cases = append(cases,
		openAPIA6Case{name: "bound session matches", status: 200, auth: "Bearer " + bound, headers: map[string]string{"User-Agent": "a6-bound"}, change: func(f *openAPIA6Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }},
		openAPIA6Case{name: "binding mismatch revokes and audits before business", status: 401, auth: "Bearer " + bound, headers: map[string]string{"User-Agent": "a6-other"}, change: func(f *openAPIA6Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }, inspect: func(t *testing.T, f *openAPIA6Fixture, _ any) {
			if fmt.Sprint(f.a1.refresh.revoked) != "[synthetic-session]" || len(f.audit.logs) != 1 || f.audit.logs[0].Action != service.AuditActionSessionBindingMismatch {
				t.Fatal("binding rejection side effects drift")
			}
		}},
		openAPIA6Case{name: "legacy unbound session", status: 200, change: func(f *openAPIA6Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }})
	if admin {
		cases = append(cases,
			openAPIA6Case{name: "stored user role beats admin claim", status: 403, change: func(f *openAPIA6Fixture) { f.a1.users.user.Role = service.RoleUser }},
			openAPIA6Case{name: "stored admin beats user claim", status: 200, auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey)},
			openAPIA6Case{name: "compliance", status: 423, change: func(f *openAPIA6Fixture) { delete(f.a1.settings.values, "admin_compliance_acknowledgement:41") }},
			openAPIA6Case{name: "compliance store failure", status: 500, change: func(f *openAPIA6Fixture) { f.a1.settings.getErrKey = "admin_compliance_acknowledgement:41" }},
			openAPIA6Case{name: "admin key", auth: "-", status: 200, headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
			openAPIA6Case{name: "invalid key blocks valid JWT", status: 401, headers: map[string]string{"x-api-key": "invalid"}},
			openAPIA6Case{name: "valid key beats invalid JWT", auth: "Bearer invalid", status: 200, headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
			openAPIA6Case{name: "first admin unavailable", status: 500, headers: map[string]string{"x-api-key": openAPIA1AdminKey}, change: func(f *openAPIA6Fixture) { f.a1.users.adminErr = true }},
			openAPIA6Case{name: "key settings unavailable", status: 500, headers: map[string]string{"x-api-key": openAPIA1AdminKey}, change: func(f *openAPIA6Fixture) { f.a1.settings.getErrKey = service.SettingKeyAdminAPIKey }},
			openAPIA6Case{name: "upgrade JWT precedence", status: 200, headers: map[string]string{"Upgrade": "websocket", "Connection": "upgrade", "Sec-WebSocket-Protocol": "sub2api-admin, jwt." + token, "x-api-key": "invalid"}},
			openAPIA6Case{name: "invalid upgrade blocks valid key", status: 401, headers: map[string]string{"Upgrade": "websocket", "Connection": "upgrade", "Sec-WebSocket-Protocol": "jwt.invalid", "x-api-key": openAPIA1AdminKey}})
	} else {
		cases = append(cases,
			openAPIA6Case{name: "backend mode", status: 403, change: func(f *openAPIA6Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
			openAPIA6Case{name: "backend stored admin", status: 200, auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey), change: func(f *openAPIA6Fixture) {
				f.a1.users.user.Role = service.RoleAdmin
				f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true"
			}},
			openAPIA6Case{name: "API key is not user auth", auth: "-", status: 401, headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
			openAPIA6Case{name: "service user failure", status: 500, change: func(f *openAPIA6Fixture) { f.users.fail = "get" }},
			openAPIA6Case{name: "subscriptions failure", status: 500, change: func(f *openAPIA6Fixture) { f.subs.fail = true }})
	}
	if detail {
		for _, id := range []string{"bad", "0", "-1", "9223372036854775808", "1.5"} {
			cases = append(cases, openAPIA6Case{name: "invalid id " + id, id: id, status: 400, invalidRequest: id != "0" && id != "-1"})
		}
		for _, id := range []string{"999", "9223372036854775807"} {
			if method == "delete" {
				cases = append(cases, openAPIA6Case{name: "missing delete is successful no-op " + id, id: id, status: 200, inspect: func(t *testing.T, f *openAPIA6Fixture, _ any) {
					if f.anns.writes != 0 || f.anns.calls != 1 {
						t.Fatal("missing delete must execute once without deleting a row")
					}
				}})
			} else {
				cases = append(cases, openAPIA6Case{name: "not found " + id, id: id, status: 404, reason: "ANNOUNCEMENT_NOT_FOUND"})
			}
		}
		for _, id := range []string{"+301", "00301"} {
			cases = append(cases, openAPIA6Case{name: "decimal spelling " + id, id: id, status: 200})
		}
	}
	if write {
		cases = append(cases, openAPIA6WriteCases(method)...)
	}
	if admin && method == "get" && (!detail || readStatus) {
		cases = append(cases, openAPIA6PaginationCases(readStatus)...)
	}
	if readStatus {
		for _, failure := range []string{"list", "read", "subs"} {
			cases = append(cases, openAPIA6Case{name: "read status failure " + failure, status: 500, change: func(f *openAPIA6Fixture) {
				switch failure {
				case "list":
					f.users.fail = "list"
				case "read":
					f.reads.fail = "users"
				case "subs":
					f.subs.fail = true
				}
			}})
		}
		cases = append(cases, openAPIA6Case{name: "eligibility ignores draft and future schedule", status: 200, change: func(f *openAPIA6Fixture) {
			openAPIA6ChangeAnn(f, func(a *service.Announcement) {
				a.Status = "draft"
				at := time.Now().Add(time.Hour)
				a.StartsAt = &at
				a.Targeting = openAPIA6Target(service.AnnouncementCondition{Type: "balance", Operator: "gte", Value: 10})
			})
			f.reads.values[[2]int64{301, 41}] = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		}, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
			rows := openAPIA5Array(t, openAPIA5Map(t, v)["items"])
			if len(rows) != 2 || openAPIA5Map(t, rows[0])["eligible"] != true || openAPIA5Map(t, rows[1])["eligible"] != false || f.reads.ann != 301 || fmt.Sprint(f.reads.ids) != "[41 42]" {
				t.Fatal("eligibility or scope drift")
			}
			if openAPIA5Map(t, rows[0])["read_at"] == nil {
				t.Fatal("read map lost")
			}
		}})
	}
	if !admin {
		cases = append(cases, openAPIA6VisibilityCases(method)...)
	}
	for i := range cases {
		c := &cases[i]
		if c.id == "" {
			c.id = "301"
		}
		if c.auth == "" {
			c.auth = "Bearer " + token
		}
		if write && c.body == "" {
			if method == "post" {
				c.body = `{"title":"Announcement","content":"Content"}`
			} else {
				c.body = `{}`
			}
		}
	}
	return cases
}
func openAPIA6VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	t.Helper()
	admin := strings.Contains(path, "/admin/")
	seen := map[string]bool{}
	for _, tc := range openAPIA6Cases(t, method, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA6NewFixture(t, admin)
			if tc.change != nil {
				tc.change(f)
			}
			r := f.router(t)
			req := httptest.NewRequest(strings.ToUpper(method), openAPIA2URL(path, tc.id)+tc.query, strings.NewReader(tc.body))
			req.RemoteAddr = "203.0.113.9:1234"
			req.Header.Set("Content-Type", "application/json")
			if tc.auth != "-" {
				req.Header.Set("Authorization", tc.auth)
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			err := openAPIValidateRequest(op, req, map[string]string{"id": tc.id}, []byte(tc.body))
			if (err != nil) != tc.invalidRequest {
				t.Fatalf("request invalid=%v error=%v", tc.invalidRequest, err)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			f.auditService.Stop()
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatalf("response contract: %v body=%s", err, rec.Body.String())
			}
			var body map[string]any
			if err := openAPIDecode(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.reason != "" && body["reason"] != tc.reason {
				t.Fatalf("reason=%v want=%s", body["reason"], tc.reason)
			}
			if strings.Contains(rec.Body.String(), "PRIVATE-A6") {
				t.Fatal("internal error leaked")
			}
			if tc.status >= 400 && (f.anns.writes != 0 || f.reads.writes != 0) {
				t.Fatal("rejected request wrote state")
			}
			if tc.status == 401 || tc.status == 403 || tc.status == 423 || tc.status == 429 {
				if f.anns.calls != 0 || f.reads.calls != 0 || len(f.subs.ids) != 0 {
					t.Fatal("rejected request reached business storage")
				}
			}
			if tc.status == 429 && rec.Header().Get("Retry-After") != "2" {
				t.Fatal("retry header drift")
			}
			if tc.status == 200 {
				if !admin {
					for _, id := range f.subs.ids {
						if id != 41 {
							t.Fatal("subscription scope spoofed")
						}
					}
					if f.reads.calls > 0 && f.reads.user != 41 {
						t.Fatal("read identity spoofed")
					}
				}
				if admin && (method == "put" || method == "post") {
					a := openAPIA5Map(t, body["data"])
					if a["updated_by"] != json.Number("41") {
						t.Fatal("actor spoofed")
					}
					if method == "post" && a["created_by"] != json.Number("41") {
						t.Fatal("creator spoofed")
					}
				}
			}
			if method != "get" && tc.status != 401 && tc.status != 403 && tc.status != 429 && tc.name != "auth store failure" && tc.name != "first admin unavailable" && tc.name != "key settings unavailable" {
				if len(f.audit.logs) != 1 {
					t.Fatalf("audit count=%d", len(f.audit.logs))
				}
				a := f.audit.logs[0]
				if a.ActorUserID == nil || *a.ActorUserID != 41 || a.ActorRole != f.a1.users.user.Role || a.StatusCode != tc.status || a.Path != strings.ReplaceAll(path, "{id}", ":id") {
					t.Fatalf("audit projection %+v", a)
				}
				if strings.Contains(a.CredentialMasked, openAPIA1SigningKey) || a.CredentialMasked == strings.TrimPrefix(tc.auth, "Bearer ") {
					t.Fatal("audit credential not masked")
				}
			}
			if tc.inspect != nil {
				tc.inspect(t, f, body["data"])
			}
			seen[fmt.Sprint(tc.status)] = true
		})
	}
	for status := range op.Responses {
		if !seen[status] {
			t.Errorf("response lacks executable case: %s", status)
		}
	}
}
func TestOpenAPIA6Announcements(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, key := range openAPIA6Routes {
		m, p, _ := strings.Cut(key, " ")
		t.Run(key, func(t *testing.T) {
			openAPIA6VerifyResponses(t, strings.ToLower(m), p, doc.Paths[p][strings.ToLower(m)])
		})
	}
}

func openAPIA6PaginationCases(readStatus bool) []openAPIA6Case {
	cases := []openAPIA6Case{}
	for _, c := range []struct {
		q                           string
		p, size                     int
		sort, order, search, status string
	}{
		{"", 1, 20, "", "", "", ""},
		{"?page=2&page=9&page_size=3&page_size=8&limit=7&sort_by=%20TITLE%20&sort_order=%20ASC%20&search=%20hello%20&status=%20active%20", 2, 3, " TITLE ", " ASC ", "hello", "active"},
		{"?page=-1&page_size=1001&limit=7", 1, 20, "", "", "", ""},
		{"?page=+2&page_size=&limit=5", 1, 5, "", "", "", ""},
		{"?page=0002&page_size=bad&limit=5&sort_by=unknown&sort_order=unknown&status=bogus", 2, 20, "unknown", "unknown", "", "bogus"},
		{"?page_size=1000", 1, 1000, "", "", "", ""},
		{"?search=" + url.QueryEscape(strings.Repeat("界", 70)), 1, 20, "", "", strings.Repeat("界", 70)[:200], ""},
	} {
		c := c
		cases = append(cases, openAPIA6Case{name: "pagination " + c.q, query: c.q, status: 200, inspect: func(t *testing.T, f *openAPIA6Fixture, v any) {
			p := f.anns.params
			search := f.anns.filters.Search
			if readStatus {
				p = f.users.params
				search = f.users.filters.Search
			}
			sortBy, order := c.sort, c.order
			if sortBy == "" {
				sortBy = "created_at"
				if readStatus {
					sortBy = "email"
				}
			}
			if order == "" {
				order = "desc"
				if readStatus {
					order = "asc"
				}
			}
			if p.Page != c.p || p.PageSize != c.size || p.SortBy != sortBy || p.SortOrder != order || search != c.search {
				t.Fatalf("pagination forwarded %+v search=%q", p, search)
			}
			if !readStatus && f.anns.filters.Status != c.status {
				t.Fatal("status trim drift")
			}
			d := openAPIA5Map(t, v)
			if d["page"] != json.Number(fmt.Sprint(c.p)) || d["page_size"] != json.Number(fmt.Sprint(c.size)) {
				t.Fatal("pagination response drift")
			}
		}})
	}
	cases = append(cases, openAPIA6Case{name: "empty page", status: 200, change: func(f *openAPIA6Fixture) {
		if readStatus {
			f.users.list = nil
		} else {
			f.anns.rows = map[int64]service.Announcement{}
		}
	}, inspect: func(t *testing.T, _ *openAPIA6Fixture, v any) {
		if len(openAPIA5Array(t, openAPIA5Map(t, v)["items"])) != 0 {
			t.Fatal("empty page drift")
		}
	}})
	return cases
}
