package routes_test

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const openAPIA10Root = "/api/v1/admin/error-passthrough-rules"

var openAPIA10Routes = []string{"GET " + openAPIA10Root, "POST " + openAPIA10Root, "GET " + openAPIA10Root + "/{id}", "PUT " + openAPIA10Root + "/{id}", "DELETE " + openAPIA10Root + "/{id}"}

func openAPIA10Operation(key string) bool {
	for _, k := range openAPIA10Routes {
		if key == k {
			return true
		}
	}
	return false
}

type openAPIA10Case struct {
	name, body, id, auth, query string
	status                      int
	invalid, guard              bool
	headers                     map[string]string
	change                      func(*openAPIA10Fixture)
	inspect                     func(*testing.T, *openAPIA10Fixture, map[string]any)
}

func openAPIA10Cases(t *testing.T, method, path string) []openAPIA10Case {
	cs := []openAPIA10Case{
		{name: "populated success", status: 200},
		{name: "missing auth", auth: "-", status: 401, guard: true},
		{name: "invalid auth", auth: "Bearer bad", status: 401, guard: true},
		{name: "stored role", status: 403, guard: true, change: func(f *openAPIA10Fixture) { f.a1.users.user.Role = service.RoleUser }},
		{name: "inactive", status: 401, guard: true, change: func(f *openAPIA10Fixture) { f.a1.users.user.Status = "disabled" }},
		{name: "revoked", status: 401, guard: true, change: func(f *openAPIA10Fixture) { f.a1.users.user.TokenVersion++ }},
		{name: "auth missing user", status: 401, guard: true, change: func(f *openAPIA10Fixture) { f.a1.users.lookupErr = service.ErrUserNotFound }},
		{name: "auth storage", status: 500, guard: true, change: func(f *openAPIA10Fixture) { f.a1.users.lookupErr = fmt.Errorf("PRIVATE-A10-AUTH") }},
		{name: "compliance", status: 423, guard: true, change: func(f *openAPIA10Fixture) { delete(f.a1.settings.values, "admin_compliance_acknowledgement:41") }},
		{name: "compliance storage", status: 500, guard: true, change: func(f *openAPIA10Fixture) { f.a1.settings.getErrKey = "admin_compliance_acknowledgement:41" }},
		{name: "admin key", auth: "-", headers: map[string]string{"x-api-key": openAPIA1AdminKey}, status: 200},
		{name: "invalid key beats JWT", headers: map[string]string{"x-api-key": "bad"}, status: 401, guard: true},
		{name: "valid key beats bad JWT", auth: "Bearer bad", headers: map[string]string{"x-api-key": openAPIA1AdminKey}, status: 200},
		{name: "Global limiter", status: 429, guard: true, change: func(f *openAPIA10Fixture) {
			f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"exempt_admin":false}`
			f.a1.counter.count = 2
		}},
		{name: "no Heavy or step-up", status: 200, change: func(f *openAPIA10Fixture) {
			f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"exempt_admin":false}`
			f.a1.counter.count = 2
		}},
		{name: "backend mode admin", status: 200, change: func(f *openAPIA10Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
		{name: "repository storage", status: 500, change: func(f *openAPIA10Fixture) { f.repo.fail = "all" }},
	}
	bound := openAPIA1Token(t, func(c *service.JWTClaims) {
		c.BindingHash = (&service.SessionBinding{IP: "203.0.113.9", UserAgent: "expected"}).Hash()
	}, openAPIA1SigningKey)
	cs = append(cs, openAPIA10Case{name: "session mismatch", status: 401, guard: true, auth: "Bearer " + bound, headers: map[string]string{"User-Agent": "changed"}, change: func(f *openAPIA10Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }})
	token := openAPIA1Token(t, nil, openAPIA1SigningKey)
	cs = append(cs, openAPIA10Case{name: "upgrade JWT still JSON", status: 200, auth: "-", headers: map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Protocol": "jwt." + token, "x-api-key": "bad"}})
	if strings.Contains(path, "{id}") {
		for _, id := range []string{"bad", "9223372036854775808", "-9223372036854775809"} {
			cs = append(cs, openAPIA10Case{name: "bad id " + id, id: id, status: 400, invalid: true})
		}
		for _, id := range []string{"0", "-1", "999"} {
			status := 404
			if method == "delete" {
				status = 500
			}
			cs = append(cs, openAPIA10Case{name: "missing id " + id, id: id, status: status})
		}
		for _, id := range []string{"+301", "00301"} {
			cs = append(cs, openAPIA10Case{name: "signed id " + id, id: id, status: 200})
		}
	}
	if method == "get" && path == openAPIA10Root {
		cs = append(cs, openAPIA10Case{name: "empty ignores queries", status: 200, query: "?page=bad&sort=desc&enabled=false", change: func(f *openAPIA10Fixture) { clear(f.repo.rows) }, inspect: func(t *testing.T, _ *openAPIA10Fixture, b map[string]any) {
			if len(openAPIA5Array(t, b["data"])) != 0 {
				t.Fatal("empty list")
			}
		}})
	}
	if method == "post" || method == "put" {
		cs = append(cs, openAPIA10Case{name: "bad JSON", body: "{", status: 400, invalid: true}, openAPIA10Case{name: "wrong root", body: "[]", status: 400, invalid: true})
		for _, field := range []string{"name", "match_mode", "conditions", "response_code", "custom_message"} {
			body := map[string]string{"name": `{"name":"","error_codes":[429]}`, "match_mode": `{"name":"x","error_codes":[429],"match_mode":"ANY"}`, "conditions": `{"name":"x","error_codes":[],"keywords":[]}`, "response_code": `{"name":"x","error_codes":[429],"passthrough_code":false,"response_code":0}`, "custom_message": `{"name":"x","error_codes":[429],"passthrough_body":false,"custom_message":""}`}[field]
			cs = append(cs, openAPIA10Case{name: "validation " + field, body: body, status: 400, invalid: method == "post" && field == "name"})
		}
		cs = append(cs, openAPIA10Case{name: "postwrite reload failure", status: 200, change: func(f *openAPIA10Fixture) { f.repo.fail = "list" }, inspect: openAPIA10Committed},
			openAPIA10Case{name: "cache errors remain success", status: 200, change: func(f *openAPIA10Fixture) { f.cache = &openAPIA10Cache{fail: true} }, inspect: openAPIA10CacheCommitted})
	}
	if method == "put" {
		cs = append(cs, openAPIA10Case{name: "write-time missing", status: 500, change: func(f *openAPIA10Fixture) { f.repo.fail = "update-missing" }})
	}
	if method == "delete" {
		cs = append(cs, openAPIA10Case{name: "delete reload failure", status: 200, change: func(f *openAPIA10Fixture) { f.repo.fail = "list" }, inspect: openAPIA10Committed}, openAPIA10Case{name: "delete cache failure", status: 200, change: func(f *openAPIA10Fixture) { f.cache = &openAPIA10Cache{fail: true} }, inspect: openAPIA10CacheCommitted})
	}
	return cs
}
func openAPIA10Committed(t *testing.T, f *openAPIA10Fixture, _ map[string]any) {
	t.Helper()
	if f.repo.writes != 1 {
		t.Fatal("expected successful write despite cache error")
	}
}
func openAPIA10CacheCommitted(t *testing.T, f *openAPIA10Fixture, b map[string]any) {
	t.Helper()
	openAPIA10Committed(t, f, b)
	if f.cache.invalidates != 1 || f.cache.notifies != 1 || f.cache.sets != 1 || f.cache.subscriptions != 1 {
		t.Fatal("cache lifecycle drift")
	}
}
func openAPIA10Run(t *testing.T, method, path string, op openAPIOperation, tc openAPIA10Case) (*openAPIA10Fixture, map[string]any) {
	t.Helper()
	f := openAPIA10NewFixture(t)
	if tc.change != nil {
		tc.change(f)
	}
	body := tc.body
	if body == "" {
		if method == "post" {
			body = `{"name":"Created","error_codes":[429]}`
		}
		if method == "put" {
			body = "{}"
		}
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
	req := httptest.NewRequest(strings.ToUpper(method), openAPIA2URL(path, id)+tc.query, strings.NewReader(body))
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
		t.Fatalf("invalid=%v error=%v", tc.invalid, e)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	f.auditService.Stop()
	if rec.Code != tc.status {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
	}
	if e := openAPIValidateResponse(op, rec); e != nil {
		t.Fatalf("response: %v body=%s", e, rec.Body.String())
	}
	var b map[string]any
	if e := openAPIDecode(rec.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(rec.Body.String(), "PRIVATE-A10") {
		t.Fatal("private error leaked")
	}
	if tc.guard && f.repo.calls != 0 {
		t.Fatal("policy guard reached request repository")
	}
	if tc.status >= 400 && f.repo.writes != 0 {
		t.Fatal("failed request mutated memory repository")
	}
	if tc.status == 429 && rec.Header().Get("Retry-After") != "2" {
		t.Fatal("Retry-After drift")
	}
	if tc.inspect != nil {
		tc.inspect(t, f, b)
	}
	return f, b
}
func openAPIA10VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	seen := map[string]bool{}
	for _, tc := range openAPIA10Cases(t, method, path) {
		t.Run(tc.name, func(t *testing.T) { openAPIA10Run(t, method, path, op, tc); seen[fmt.Sprint(tc.status)] = true })
	}
	for s := range op.Responses {
		if !seen[s] {
			t.Errorf("no runtime witness for %s", s)
		}
	}
}
func TestOpenAPIA10LifecycleResponses(t *testing.T) {
	d, _ := openAPILoad(t)
	for _, key := range openAPIA10Routes {
		parts := strings.SplitN(key, " ", 2)
		m := strings.ToLower(parts[0])
		p := parts[1]
		t.Run(key, func(t *testing.T) { openAPIA10VerifyResponses(t, m, p, d.Paths[p][m]) })
	}
}
