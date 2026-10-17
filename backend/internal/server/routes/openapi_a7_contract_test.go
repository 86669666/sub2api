package routes_test

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var openAPIA7Routes = []string{"GET /api/v1/admin/channel-monitor-v2/config", "PUT /api/v1/admin/channel-monitor-v2/config", "GET /api/v1/admin/channel-monitor-v2/dimensions", "GET /api/v1/admin/channel-monitor-v2/snapshot", "GET /api/v1/admin/channel-monitor-v2/models", "GET /api/v1/admin/channel-monitor-v2/matrix", "GET /api/v1/admin/channel-monitor-v2/errors", "GET /api/v1/admin/channel-monitor-v2/users"}

func openAPIA7Operation(key string) bool {
	for _, k := range openAPIA7Routes {
		if k == key {
			return true
		}
	}
	return false
}

type openAPIA7Case struct {
	name, query, body, auth, reason string
	headers                         map[string]string
	status                          int
	invalidRequest                  bool
	change                          func(*openAPIA7Fixture)
	inspect                         func(*testing.T, *openAPIA7Fixture, any)
}

func openAPIA7Cases(t *testing.T, method, path string) []openAPIA7Case {
	cfg := strings.HasSuffix(path, "/config")
	write := method == "put"
	token := openAPIA1Token(t, nil, openAPIA1SigningKey)
	limit := func(f *openAPIA7Fixture) {
		f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"exempt_admin":false}`
		f.a1.counter.count = 2
	}
	cases := []openAPIA7Case{
		{name: "populated", status: 200},
		{name: "missing auth", auth: "-", status: 401},
		{name: "invalid auth", auth: "Bearer invalid", status: 401},
		{name: "wrong scheme", auth: "Basic invalid", status: 401},
		{name: "expired", auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey), status: 401},
		{name: "revoked", status: 401, change: func(f *openAPIA7Fixture) { f.a1.users.user.TokenVersion++ }},
		{name: "inactive", status: 401, change: func(f *openAPIA7Fixture) { f.a1.users.user.Status = "disabled" }},
		{name: "missing user", status: 401, change: func(f *openAPIA7Fixture) { f.a1.users.lookupErr = service.ErrUserNotFound }},
		{name: "auth failure", status: 500, change: func(f *openAPIA7Fixture) { f.a1.users.lookupErr = fmt.Errorf("PRIVATE-A7-AUTH") }},
		{name: "stored user beats admin claim", status: 403, change: func(f *openAPIA7Fixture) { f.a1.users.user.Role = service.RoleUser }},
		{name: "stored admin beats user claim", status: 200, auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey)},
		{name: "compliance", status: 423, change: func(f *openAPIA7Fixture) { delete(f.a1.settings.values, "admin_compliance_acknowledgement:41") }},
		{name: "compliance storage failure", status: 500, change: func(f *openAPIA7Fixture) { f.a1.settings.getErrKey = "admin_compliance_acknowledgement:41" }},
		{name: "admin key", auth: "-", status: 200, headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
		{name: "invalid key blocks JWT", status: 401, headers: map[string]string{"x-api-key": "invalid"}},
		{name: "valid key beats invalid JWT", auth: "Bearer invalid", status: 200, headers: map[string]string{"x-api-key": openAPIA1AdminKey}},
		{name: "first admin unavailable", status: 500, headers: map[string]string{"x-api-key": openAPIA1AdminKey}, change: func(f *openAPIA7Fixture) { f.a1.users.adminErr = true }},
		{name: "key setting unavailable", status: 500, headers: map[string]string{"x-api-key": openAPIA1AdminKey}, change: func(f *openAPIA7Fixture) { f.a1.settings.getErrKey = service.SettingKeyAdminAPIKey }},
		{name: "upgrade precedence", status: 200, headers: map[string]string{"Upgrade": "websocket", "Connection": "upgrade", "Sec-WebSocket-Protocol": "jwt." + token, "x-api-key": "invalid"}},
		{name: "bad upgrade blocks key", status: 401, headers: map[string]string{"Upgrade": "websocket", "Connection": "upgrade", "Sec-WebSocket-Protocol": "jwt.invalid", "x-api-key": openAPIA1AdminKey}},
		{name: "global limiter", status: 429, change: limit},
		{name: "limiter fail open", status: 200, change: func(f *openAPIA7Fixture) { limit(f); f.a1.counter.fail = true }},
		{name: "no heavy limiter", status: 200, change: func(f *openAPIA7Fixture) {
			f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"exempt_admin":false}`
			f.a1.counter.count = 2
		}},
		{name: "backend mode admin allowed", status: 200, change: func(f *openAPIA7Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
		{name: "identity spoof ignored", status: 200, query: "?user_id=42&role=user&group_by=ignored", headers: map[string]string{"X-User-ID": "42", "X-User-Role": "user"}},
		{name: "feature disabled", status: 403, reason: "CHANNEL_MONITOR_DISABLED", change: func(f *openAPIA7Fixture) { f.a1.settings.values[service.SettingKeyChannelMonitorEnabled] = "false" }},
		{name: "repository failure", status: 500, change: func(f *openAPIA7Fixture) { f.v2.fail = true; f.v2.configFail = true }},
	}
	bound := openAPIA1Token(t, func(c *service.JWTClaims) {
		c.BindingHash = (&service.SessionBinding{IP: "203.0.113.9", UserAgent: "a7-bound"}).Hash()
	}, openAPIA1SigningKey)
	cases = append(cases, openAPIA7Case{name: "binding mismatch", status: 401, auth: "Bearer " + bound, headers: map[string]string{"User-Agent": "different"}, change: func(f *openAPIA7Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
		if fmt.Sprint(f.a1.refresh.revoked) != "[synthetic-session]" || len(f.audit.logs) != 1 {
			t.Fatal("binding audit/revoke drift")
		}
	}})
	for _, mode := range []string{"v1", "unknown", ""} {
		status := 403
		if cfg {
			status = 200
		}
		cases = append(cases, openAPIA7Case{name: "mode " + mode, status: status, change: func(f *openAPIA7Fixture) { f.a1.settings.values[service.SettingKeyChannelMonitorMode] = mode }})
	}
	settingsStatus := 403
	if cfg {
		settingsStatus = 200
	}
	cases = append(cases, openAPIA7Case{name: "runtime settings failure", status: settingsStatus, change: func(f *openAPIA7Fixture) { f.a1.settings.multipleErr = true }})
	status := 403
	if cfg {
		status = 200
	}
	cases = append(cases, openAPIA7Case{name: "stored config disabled", status: status, change: func(f *openAPIA7Fixture) { f.v2.config.Enabled = false }})
	if !write {
		cases = append(cases, openAPIA7Case{name: "nil config", status: status, change: func(f *openAPIA7Fixture) { f.v2.configNil = true }})
	}
	if write {
		cases = append(cases, openAPIA7WriteCases()...)
	}
	if !cfg {
		cases = append(cases, openAPIA7QueryCases(path)...)
	}
	for i := range cases {
		if cases[i].auth == "" {
			cases[i].auth = "Bearer " + token
		}
		if write && cases[i].body == "" {
			cases[i].body = `{"version":2,"enabled":true}`
		}
		if strings.HasSuffix(path, "/matrix") && cases[i].name == "identity spoof ignored" {
			cases[i].query = "?user_id=42&role=user"
		}
	}
	return cases
}
func openAPIA7VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range openAPIA7Cases(t, method, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA7NewFixture(t)
			if tc.change != nil {
				tc.change(f)
			}
			r := f.router(t)
			req := httptest.NewRequest(strings.ToUpper(method), path+tc.query, strings.NewReader(tc.body))
			req.RemoteAddr = "203.0.113.9:1234"
			req.Header.Set("Content-Type", "application/json")
			if tc.auth != "-" {
				req.Header.Set("Authorization", tc.auth)
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			err := openAPIValidateRequest(op, req, nil, []byte(tc.body))
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
				t.Fatal("reason drift")
			}
			if strings.Contains(rec.Body.String(), "PRIVATE-A7") {
				t.Fatal("internal error leaked")
			}
			if tc.status == 400 && f.v2.updates+f.v2.configCalls+f.v2.calls != 0 {
				t.Fatal("validation failure reached repository")
			}
			if tc.status >= 400 && f.v2.writes != 0 {
				t.Fatal("failed request mutated config")
			}
			if tc.status == 401 || tc.status == 423 || tc.status == 429 || tc.name == "stored user beats admin claim" || tc.name == "feature disabled" || strings.HasPrefix(tc.name, "mode ") {
				if tc.status != 200 && (f.v2.calls+f.v2.configCalls+f.v2.updates) != 0 {
					t.Fatal("guard reached repository")
				}
			}
			if tc.status == 429 && rec.Header().Get("Retry-After") != "2" {
				t.Fatal("retry header drift")
			}
			if tc.status == 200 {
				if f.v2.filter.RestrictGroups || len(f.v2.filter.AllowedGroupIDs) != 0 {
					t.Fatal("admin incorrectly restricted by AvailableGroups")
				}
				if method == "put" {
					if f.v2.input.UpdatedBy == nil || *f.v2.input.UpdatedBy != 41 {
						t.Fatal("stored subject not used")
					}
				}
				if tc.name == "populated" || tc.name == "privacy flags ignored" || tc.name == "privacy reader absent" {
					openAPIA7Projection(t, path, body["data"])
				}
			}
			if method == "put" && f.v2.updates > 0 {
				if len(f.audit.logs) != 1 || f.audit.logs[0].ActorUserID == nil || *f.audit.logs[0].ActorUserID != 41 || f.audit.logs[0].StatusCode != tc.status {
					t.Fatal("write audit drift")
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
func openAPIA7Projection(t *testing.T, path string, v any) {
	b := openAPIA5Map(t, v)
	metric := func(v any) {
		m := openAPIA5Map(t, v)
		for k, w := range map[string]string{"request_count": "100", "input_tokens": "110", "output_tokens": "120", "cache_creation_tokens": "130", "cache_read_tokens": "140", "token_count": "500", "rpm": "2.5", "tpm": "12.5", "cache_rate_numerator": "50", "cache_rate_denominator": "100", "upstream_affected_requests": "9", "upstream_attempt_count": "9"} {
			if m[k] != json.Number(w) {
				t.Fatalf("admin metric %s=%v want=%s", k, m[k], w)
			}
		}
		if openAPIA5Map(t, m["ttft"])["sample_count"] != json.Number("100") {
			t.Fatal("sample count redacted")
		}
	}
	switch {
	case strings.HasSuffix(path, "/config"):
	case strings.HasSuffix(path, "/snapshot"):
		c := openAPIA5Map(t, b["config"])
		if c["updated_by"] != json.Number("99") || len(openAPIA5Array(t, c["group_ids"])) != 2 || c["ignored_error_categories"] == nil {
			t.Fatal("admin policy redacted")
		}
		metric(b["metrics"])
		metric(openAPIA5Map(t, openAPIA5Array(t, b["trend"])[0])["metrics"])
	case strings.HasSuffix(path, "/dimensions"):
		for _, key := range []string{"platforms", "groups", "models"} {
			if openAPIA5Map(t, openAPIA5Array(t, b[key])[0])["request_count"] != json.Number("100") {
				t.Fatal("dimension count redacted")
			}
		}
	case strings.HasSuffix(path, "/errors"):
		row := openAPIA5Map(t, openAPIA5Array(t, b["items"])[0])
		detail := openAPIA5Map(t, openAPIA5Array(t, row["details"])[0])
		if row["count"] != json.Number("10") || detail["message"] != "ADMIN-DETAIL-CANARY" || detail["upstream_status_code"] != json.Number("503") {
			t.Fatal("admin diagnostics redacted")
		}
	default:
		for _, v := range openAPIA5Array(t, b["items"]) {
			row := openAPIA5Map(t, v)
			metric(row["metrics"])
			if strings.HasSuffix(path, "/users") && row["user_id"] == nil {
				t.Fatal("admin identity redacted")
			}
			if buckets, ok := row["buckets"]; ok {
				for _, v := range openAPIA5Array(t, buckets) {
					metric(openAPIA5Map(t, v)["metrics"])
				}
			}
		}
	}
}
func TestOpenAPIA7AdminMonitorV2(t *testing.T) {
	d, _ := openAPILoad(t)
	for _, key := range openAPIA7Routes {
		m, p, _ := strings.Cut(key, " ")
		t.Run(key, func(t *testing.T) { openAPIA7VerifyResponses(t, strings.ToLower(m), p, d.Paths[p][strings.ToLower(m)]) })
	}
}
