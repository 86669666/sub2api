package routes_test

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http/httptest"
	"strings"
	"testing"
)

var openAPIA8Routes = []string{
	"GET /api/v1/admin/channel-monitors", "POST /api/v1/admin/channel-monitors",
	"GET /api/v1/admin/channel-monitors/{id}", "PUT /api/v1/admin/channel-monitors/{id}", "DELETE /api/v1/admin/channel-monitors/{id}",
	"POST /api/v1/admin/channel-monitors/{id}/duplicate", "POST /api/v1/admin/channel-monitors/{id}/run", "GET /api/v1/admin/channel-monitors/{id}/history",
	"GET /api/v1/admin/channel-monitor-templates", "POST /api/v1/admin/channel-monitor-templates",
	"GET /api/v1/admin/channel-monitor-templates/{id}", "PUT /api/v1/admin/channel-monitor-templates/{id}", "DELETE /api/v1/admin/channel-monitor-templates/{id}",
	"GET /api/v1/admin/channel-monitor-templates/{id}/monitors", "POST /api/v1/admin/channel-monitor-templates/{id}/apply",
}

func openAPIA8Operation(key string) bool {
	for _, k := range openAPIA8Routes {
		if k == key {
			return true
		}
	}
	return false
}

type openAPIA8Case struct {
	name, body, query, id, auth, reason string
	headers                             map[string]string
	status                              int
	invalidRequest                      bool
	change                              func(*openAPIA8Fixture)
	inspect                             func(*testing.T, *openAPIA8Fixture, any)
}

func openAPIA8Body(method, path string) string {
	if method == "post" && strings.HasSuffix(path, "/apply") {
		return `{"monitor_ids":[301]}`
	}
	if method == "post" && !strings.Contains(path, "{id}") {
		if strings.Contains(path, "templates") {
			return `{"name":" Template ","provider":"openai"}`
		}
		return `{"name":" Monitor ","provider":"openai","endpoint":"https://8.8.8.8/base","api_key":"` + openAPIA8Secret + `","primary_model":" test-model ","interval_seconds":60}`
	}
	if method == "put" {
		return `{}`
	}
	return ""
}
func openAPIA8Cases(t *testing.T, method, path string) []openAPIA8Case {
	tpl := strings.Contains(path, "templates")
	detail := strings.Contains(path, "{id}")
	run := strings.HasSuffix(path, "/run")
	dup := strings.HasSuffix(path, "/duplicate")
	create := method == "post" && !detail
	success := 200
	if create {
		success = 201
	}
	cases := []openAPIA8Case{
		{name: "populated", status: success},
		{name: "missing auth", auth: "-", status: 401},
		{name: "invalid auth", auth: "Bearer invalid", status: 401},
		{name: "stored role", status: 403, change: func(f *openAPIA8Fixture) { f.a1.users.user.Role = service.RoleUser }},
		{name: "revoked", status: 401, change: func(f *openAPIA8Fixture) { f.a1.users.user.TokenVersion++ }},
		{name: "inactive", status: 401, change: func(f *openAPIA8Fixture) { f.a1.users.user.Status = "disabled" }},
		{name: "auth storage failure", status: 500, change: func(f *openAPIA8Fixture) { f.a1.users.lookupErr = fmt.Errorf("PRIVATE-A8-AUTH") }},
		{name: "compliance", status: 423, change: func(f *openAPIA8Fixture) { delete(f.a1.settings.values, "admin_compliance_acknowledgement:41") }},
		{name: "compliance storage failure", status: 500, change: func(f *openAPIA8Fixture) { f.a1.settings.getErrKey = "admin_compliance_acknowledgement:41" }},
		{name: "admin key", status: success, auth: "-", headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
		{name: "invalid key beats JWT", status: 401, headers: map[string]string{"x-api-key": "invalid"}},
		{name: "valid key beats invalid JWT", status: success, auth: "Bearer invalid", headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
		{name: "feature disabled", status: 403, reason: "CHANNEL_MONITOR_DISABLED", change: func(f *openAPIA8Fixture) { f.a1.settings.values[service.SettingKeyChannelMonitorEnabled] = "false" }},
		{name: "global limiter", status: 429, change: func(f *openAPIA8Fixture) {
			f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"exempt_admin":false}`
			f.a1.counter.count = 2
		}},
		{name: "no heavy limiter", status: success, change: func(f *openAPIA8Fixture) {
			f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"exempt_admin":false}`
			f.a1.counter.count = 2
		}},
		{name: "backend mode allowed", status: success, change: func(f *openAPIA8Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
		{name: "repository failure", status: 500, change: func(f *openAPIA8Fixture) { f.monitors.failOp = "all"; f.templates.fail = "all" }},
	}
	v2 := success
	if run {
		v2 = 403
	}
	cases = append(cases, openAPIA8Case{name: "v2 mode", status: v2, change: func(f *openAPIA8Fixture) { f.a1.settings.values[service.SettingKeyChannelMonitorMode] = "v2" }})
	if detail {
		for _, id := range []string{"0", "-1", "bad", "9223372036854775808"} {
			cases = append(cases, openAPIA8Case{name: "invalid ID " + id, id: id, status: 400, invalidRequest: id == "bad" || id == "9223372036854775808"})
		}
		cases = append(cases, openAPIA8Case{name: "missing row", id: "9999", status: 404})
	}
	if method == "get" && !detail {
		cases = append(cases, openAPIA8Case{name: "empty list", status: 200, change: func(f *openAPIA8Fixture) { f.monitors.empty = true; f.templates.empty = true }, inspect: func(t *testing.T, _ *openAPIA8Fixture, v any) {
			if len(openAPIA5Array(t, openAPIA5Map(t, v)["items"])) != 0 {
				t.Fatal("nonempty list")
			}
		}})
	}
	if create || method == "put" || strings.HasSuffix(path, "/apply") {
		cases = append(cases, openAPIA8Case{name: "malformed JSON", body: `{`, status: 400, invalidRequest: true}, openAPIA8Case{name: "wrong JSON type", body: `[]`, status: 400, invalidRequest: true})
	}
	if tpl && method == "get" && !detail {
		cases = append(cases, openAPIA8Case{name: "invalid filter", query: "?provider=invalid", status: 400})
	}
	if dup {
		cases = append(cases,
			openAPIA8Case{name: "key required", status: 400, change: func(f *openAPIA8Fixture) { f.enforceIdem = true }},
			openAPIA8Case{name: "key invalid", status: 400, headers: map[string]string{"Idempotency-Key": "bad key"}},
			openAPIA8Case{name: "in progress", status: 409, headers: map[string]string{"Idempotency-Key": "synthetic-duplicate"}, change: func(f *openAPIA8Fixture) { f.idem.forcedStatus = service.IdempotencyStatusProcessing }},
			openAPIA8Case{name: "store unavailable", status: 503, headers: map[string]string{"Idempotency-Key": "synthetic-duplicate"}, change: func(f *openAPIA8Fixture) { f.idem.fail = true }},
			openAPIA8Case{name: "nil coordinator", status: 200, change: func(f *openAPIA8Fixture) { f.noCoordinator = true }},
			openAPIA8Case{name: "decrypt failure", status: 500, change: func(f *openAPIA8Fixture) { f.encryptor.decryptFail = true }},
			openAPIA8Case{name: "encrypt failure", status: 500, change: func(f *openAPIA8Fixture) { f.encryptor.encryptFail = true }},
		)
	}
	for i := range cases {
		if cases[i].body == "" {
			cases[i].body = openAPIA8Body(method, path)
		}
		if cases[i].id == "" {
			cases[i].id = "301"
			if tpl {
				cases[i].id = "501"
			}
		}
		if cases[i].auth == "" {
			cases[i].auth = "Bearer " + openAPIA1Token(t, nil, openAPIA1SigningKey)
		}
	}
	return cases
}
func openAPIA8VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	seen := map[string]bool{}
	for _, tc := range openAPIA8Cases(t, method, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA8NewFixture(t)
			// All live /run cases use quota with nil fetcher, never probe or quota_probe.
			if strings.HasSuffix(path, "/run") {
				m := f.monitors.rows[301]
				m.CheckMode = "quota"
				id := int64(77)
				m.AccountID = &id
				f.monitors.rows[301] = m
			}
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
			params := map[string]string{}
			if strings.Contains(path, "{id}") {
				params["id"] = tc.id
			}
			if e := openAPIValidateRequest(op, req, params, []byte(tc.body)); (e != nil) != tc.invalidRequest {
				t.Fatalf("request invalid=%v error=%v", tc.invalidRequest, e)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			f.auditService.Stop()
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if e := openAPIValidateResponse(op, rec); e != nil {
				t.Fatalf("response contract: %v body=%s", e, rec.Body.String())
			}
			var b map[string]any
			if e := openAPIDecode(rec.Body.Bytes(), &b); e != nil {
				t.Fatal(e)
			}
			if tc.reason != "" && b["reason"] != tc.reason {
				t.Fatal("reason drift")
			}
			if strings.Contains(rec.Body.String(), openAPIA8Secret) || strings.Contains(rec.Body.String(), "cipher:") || strings.Contains(rec.Body.String(), "PRIVATE-A8") {
				t.Fatal("private value leaked")
			}
			if rec.Code >= 400 && f.monitors.writes+f.templates.writes != 0 {
				t.Fatal("failed request wrote repository")
			}
			if rec.Code == 401 || rec.Code == 423 || rec.Code == 429 || tc.name == "stored role" || tc.name == "feature disabled" || (tc.name == "v2 mode" && rec.Code == 403) {
				if f.monitors.calls+f.templates.calls != 0 {
					t.Fatal("guard reached repository")
				}
			}
			if rec.Code == 429 && rec.Header().Get("Retry-After") != "2" {
				t.Fatal("retry header drift")
			}
			if rec.Code < 300 && (method == "post" && !strings.Contains(path, "{id}") || strings.HasSuffix(path, "/duplicate")) {
				if !strings.Contains(path, "templates") && openAPIA5Map(t, b["data"])["created_by"] != json.Number("41") {
					t.Fatal("actor spoof drift")
				}
			}
			if strings.HasSuffix(path, "/run") && rec.Code == 200 {
				if len(f.monitors.persisted) != 1 || f.monitors.checked != 1 {
					t.Fatal("quota persistence drift")
				}
				results := openAPIA5Array(t, openAPIA5Map(t, b["data"])["results"])
				if len(results) != 1 {
					t.Fatal("quota results drift")
				}
				quota := openAPIA5Map(t, openAPIA5Map(t, results[0])["quota"])
				if quota["success"] != false {
					t.Fatal("nil fetcher did not fail closed")
				}
			}
			if tc.inspect != nil {
				tc.inspect(t, f, b["data"])
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
