package routes_test

import (
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http/httptest"
	"strings"
	"testing"
)

var openAPIA9Routes = []string{"GET /api/v1/admin/channels", "POST /api/v1/admin/channels", "GET /api/v1/admin/channels/{id}", "PUT /api/v1/admin/channels/{id}", "DELETE /api/v1/admin/channels/{id}", "GET /api/v1/admin/channels/model-pricing", "GET /api/v1/admin/channels/pricing/sync-models", "GET /api/v1/channels/available"}

func openAPIA9Operation(key string) bool {
	for _, k := range openAPIA9Routes {
		if key == k {
			return true
		}
	}
	return false
}

type openAPIA9Case struct {
	name, body, query, id, auth, reason string
	status                              int
	invalid                             bool
	headers                             map[string]string
	change                              func(*openAPIA9Fixture)
	inspect                             func(*testing.T, *openAPIA9Fixture, map[string]any)
}

func openAPIA9Defaults(method, path string) (string, string) {
	body, query := "", ""
	if method == "post" {
		body = `{"name":"Created"}`
	}
	if method == "put" {
		body = `{}`
	}
	if strings.HasSuffix(path, "model-pricing") {
		query = "?model=claude-sonnet-4"
	}
	if strings.HasSuffix(path, "sync-models") {
		query = "?platform=anthropic"
	}
	return body, query
}
func openAPIA9Cases(method, path string) []openAPIA9Case {
	user := !strings.Contains(path, "/admin/")
	detail := strings.Contains(path, "{id}")
	write := method == "post" || method == "put"
	pricing := strings.HasSuffix(path, "model-pricing")
	sync := strings.HasSuffix(path, "sync-models")
	cs := []openAPIA9Case{{name: "success", status: 200}, {name: "missing auth", auth: "-", status: 401}, {name: "invalid auth", auth: "Bearer invalid", status: 401}, {name: "revoked", status: 401, change: func(f *openAPIA9Fixture) { f.a1.users.user.TokenVersion++ }}, {name: "inactive", status: 401, change: func(f *openAPIA9Fixture) { f.a1.users.user.Status = "disabled" }}, {name: "auth failure", status: 500, change: func(f *openAPIA9Fixture) { f.a1.users.lookupErr = fmt.Errorf("PRIVATE-A9-AUTH") }}, {name: "global limiter", status: 429, change: func(f *openAPIA9Fixture) {
		f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"exempt_admin":false}`
		f.a1.counter.count = 2
	}}, {name: "no heavy limiter", status: 200, change: func(f *openAPIA9Fixture) {
		f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"exempt_admin":false}`
		f.a1.counter.count = 2
	}}}
	if user {
		cs = append(cs, openAPIA9Case{name: "ordinary user", status: 200, change: func(f *openAPIA9Fixture) { f.a1.users.user.Role = service.RoleUser }}, openAPIA9Case{name: "backend blocked", status: 403, change: func(f *openAPIA9Fixture) {
			f.a1.users.user.Role = service.RoleUser
			f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true"
		}}, openAPIA9Case{name: "feature disabled", status: 200, change: func(f *openAPIA9Fixture) { f.a1.settings.values[service.SettingKeyAvailableChannelsEnabled] = "false" }, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
			if len(openAPIA5Array(t, b["data"])) != 0 || f.repo.calls != 0 {
				t.Fatal("feature guard drift")
			}
		}}, openAPIA9Case{name: "subscription failure", status: 500, change: func(f *openAPIA9Fixture) { f.subs.fail = true }}, openAPIA9Case{name: "group failure", status: 500, change: func(f *openAPIA9Fixture) { f.groups.fail = true }})
	} else {
		cs = append(cs, openAPIA9Case{name: "stored role", status: 403, change: func(f *openAPIA9Fixture) { f.a1.users.user.Role = service.RoleUser }}, openAPIA9Case{name: "compliance", status: 423, change: func(f *openAPIA9Fixture) { delete(f.a1.settings.values, "admin_compliance_acknowledgement:41") }}, openAPIA9Case{name: "compliance failure", status: 500, change: func(f *openAPIA9Fixture) { f.a1.settings.getErrKey = "admin_compliance_acknowledgement:41" }}, openAPIA9Case{name: "admin key", status: 200, auth: "-", headers: map[string]string{"x-api-key": openAPIA1AdminKey}}, openAPIA9Case{name: "invalid key beats JWT", status: 401, headers: map[string]string{"x-api-key": "invalid"}}, openAPIA9Case{name: "valid key beats invalid JWT", status: 200, auth: "Bearer invalid", headers: map[string]string{"x-api-key": openAPIA1AdminKey}}, openAPIA9Case{name: "backend admin allowed", status: 200, change: func(f *openAPIA9Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }})
	}
	if !pricing && !sync {
		cs = append(cs, openAPIA9Case{name: "repository failure", status: 500, change: func(f *openAPIA9Fixture) { f.repo.fail = "all" }})
	}
	if detail {
		for _, id := range []string{"0", "-1", "9999"} {
			cs = append(cs, openAPIA9Case{name: "missing " + id, id: id, status: 404})
		}
		for _, id := range []string{"bad", "9223372036854775808"} {
			cs = append(cs, openAPIA9Case{name: "invalid " + id, id: id, status: 400, invalid: true})
		}
		for _, id := range []string{"+301", "00301"} {
			cs = append(cs, openAPIA9Case{name: "accepted " + id, id: id, status: 200})
		}
	}
	if write {
		cs = append(cs, openAPIA9Case{name: "malformed", body: `{`, status: 400, invalid: true}, openAPIA9Case{name: "wrong body type", body: `[]`, status: 400, invalid: true}, openAPIA9Case{name: "duplicate name", body: `{"name":"Visible"}`, status: 409, change: func(f *openAPIA9Fixture) {
			v := f.repo.rows[301]
			v.ID = 302
			v.Name = "Visible"
			f.repo.rows[302] = v
			if method == "put" {
				v = f.repo.rows[301]
				v.Name = "Before"
				f.repo.rows[301] = v
			}
		}}, openAPIA9Case{name: "group conflict", body: `{"name":"Created","group_ids":[13]}`, status: 409, change: func(f *openAPIA9Fixture) { f.repo.conflict = true }}, openAPIA9Case{name: "long name", body: `{"name":"` + strings.Repeat("界", 101) + `"}`, status: 400, invalid: true}, openAPIA9Case{name: "invalid billing source", body: `{"name":"Created","billing_model_source":"invalid"}`, status: 400, invalid: true})
	}
	if pricing {
		cs = append(cs, openAPIA9Case{name: "missing model", query: "?model=%20", status: 400}, openAPIA9Case{name: "not found", query: "?model=synthetic-never-known-a9", status: 200, inspect: func(t *testing.T, _ *openAPIA9Fixture, b map[string]any) {
			m := openAPIA5Map(t, b["data"])
			if len(m) != 1 || m["found"] != false {
				t.Fatal("not-found shape drift")
			}
		}}, openAPIA9Case{name: "trim first model", query: "?model=%20claude-sonnet-4%20&model=unknown", status: 200})
	}
	if sync {
		for _, q := range []string{"?platform=", "?platform=composite", "?platform=unknown"} {
			cs = append(cs, openAPIA9Case{name: "invalid platform " + q, query: q, status: 400})
		}
		cs = append(cs, openAPIA9Case{name: "normalized platform", query: "?platform=%20AnThRoPiC%20&platform=invalid", status: 200, inspect: func(t *testing.T, _ *openAPIA9Fixture, b map[string]any) {
			if len(openAPIA5Array(t, openAPIA5Map(t, b["data"])["models"])) != 0 {
				t.Fatal("uninitialized catalog should be empty")
			}
		}})
	}
	if method == "post" {
		cs = append(cs, openAPIA9Case{name: "post-commit missing", status: 404, change: func(f *openAPIA9Fixture) { f.repo.fail = "get-missing" }, inspect: func(t *testing.T, f *openAPIA9Fixture, _ map[string]any) {
			if f.repo.writes != 1 {
				t.Fatal("expected committed create before reread failure")
			}
		}})
	}
	return cs
}
func openAPIA9Run(t *testing.T, method, path string, op openAPIOperation, tc openAPIA9Case) (*openAPIA9Fixture, map[string]any) {
	t.Helper()
	f := openAPIA9NewFixture(t)
	if tc.change != nil {
		tc.change(f)
	}
	body, query := openAPIA9Defaults(method, path)
	if tc.body != "" {
		body = tc.body
	}
	if tc.query != "" {
		query = tc.query
	}
	id := tc.id
	if id == "" {
		id = "301"
	}
	auth := tc.auth
	if auth == "" {
		auth = "Bearer " + openAPIA1Token(t, nil, openAPIA1SigningKey)
	}
	r := f.router(t)
	req := httptest.NewRequest(strings.ToUpper(method), openAPIA2URL(path, id)+query, strings.NewReader(body))
	req.RemoteAddr = "203.0.113.9:1234"
	req.Header.Set("Content-Type", "application/json")
	if auth != "-" {
		req.Header.Set("Authorization", auth)
	}
	for k, v := range tc.headers {
		req.Header.Set(k, v)
	}
	params := map[string]string{}
	if strings.Contains(path, "{id}") {
		params["id"] = id
	}
	if e := openAPIValidateRequest(op, req, params, []byte(body)); (e != nil) != tc.invalid {
		t.Fatalf("request invalid=%v err=%v", tc.invalid, e)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	f.auditService.Stop()
	if rec.Code != tc.status {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
	}
	if e := openAPIValidateResponse(op, rec); e != nil {
		t.Fatalf("contract: %v body=%s", e, rec.Body.String())
	}
	var b map[string]any
	if e := openAPIDecode(rec.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(rec.Body.String(), "PRIVATE-A9") {
		t.Fatal("private error leak")
	}
	if tc.status >= 400 && f.repo.writes != 0 && tc.name != "post-commit missing" {
		t.Fatal("failed request wrote memory repository")
	}
	if tc.status == 401 || tc.status == 423 || tc.status == 429 || tc.name == "stored role" || tc.name == "backend blocked" {
		if f.repo.calls != 0 {
			t.Fatal("guard reached channel repository")
		}
	}
	if tc.status == 429 && rec.Header().Get("Retry-After") != "2" {
		t.Fatal("retry header drift")
	}
	if tc.reason != "" && b["reason"] != tc.reason {
		t.Fatalf("reason=%v want=%s", b["reason"], tc.reason)
	}
	if tc.inspect != nil {
		tc.inspect(t, f, b)
	}
	return f, b
}
func openAPIA9VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	seen := map[string]bool{}
	for _, tc := range openAPIA9Cases(method, path) {
		t.Run(tc.name, func(t *testing.T) { openAPIA9Run(t, method, path, op, tc); seen[fmt.Sprint(tc.status)] = true })
	}
	for s := range op.Responses {
		if !seen[s] {
			t.Errorf("no runtime response witness: %s", s)
		}
	}
}
