package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/golang-jwt/jwt/v5"
)

var openAPIA5Routes = []string{"GET /api/v1/channel-monitors", "GET /api/v1/channel-monitors/{id}/status", "GET /api/v1/channel-monitor-v2/dimensions", "GET /api/v1/channel-monitor-v2/snapshot", "GET /api/v1/channel-monitor-v2/models", "GET /api/v1/channel-monitor-v2/matrix", "GET /api/v1/channel-monitor-v2/errors", "GET /api/v1/channel-monitor-v2/users"}

func openAPIA5Operation(key string) bool {
	for _, s := range openAPIA5Routes {
		if key == s {
			return true
		}
	}
	return false
}

type openAPIA5Case struct {
	name, id, query, auth, reason string
	status                        int
	invalidRequest                bool
	change                        func(*openAPIA5Fixture)
	inspect                       func(*testing.T, *openAPIA5Fixture, map[string]any)
}

func openAPIA5Map(t *testing.T, v any) map[string]any {
	t.Helper()
	return openAPIA1As[map[string]any](t, v)
}
func openAPIA5Array(t *testing.T, v any) []any { t.Helper(); return openAPIA1As[[]any](t, v) }
func openAPIA5Cases(t *testing.T, path string) []openAPIA5Case {
	v2 := stringsContainsV2(path)
	detail := strings.Contains(path, "{id}")
	cases := []openAPIA5Case{
		{name: "populated", status: 200},
		{name: "empty repository", status: 200, change: func(f *openAPIA5Fixture) {
			f.legacy.empty = true
			f.v2.empty = true
			f.legacy.latest = nil
			f.legacy.history = nil
			f.legacy.availability = nil
		}},
		{name: "missing auth", auth: "-", status: 401},
		{name: "wrong scheme", auth: "Basic invalid", status: 401},
		{name: "invalid token", auth: "Bearer invalid", status: 401},
		{name: "expired token", auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey), status: 401},
		{name: "revoked token", status: 401, change: func(f *openAPIA5Fixture) { f.base.a1.users.user.TokenVersion++ }},
		{name: "inactive user", status: 401, change: func(f *openAPIA5Fixture) { f.base.a1.users.user.Status = "disabled" }},
		{name: "backend mode", status: 403, change: func(f *openAPIA5Fixture) { f.base.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
		{name: "auth storage failure", status: 500, change: func(f *openAPIA5Fixture) { f.base.a1.users.lookupErr = fmt.Errorf("synthetic auth unavailable") }},
		{name: "global limiter", status: 429, change: func(f *openAPIA5Fixture) { f.base.limit() }},
		{name: "limiter fails open", status: 200, change: func(f *openAPIA5Fixture) { f.base.limit(); f.base.a1.counter.fail = true }},
		{name: "repository unavailable", status: 500, change: func(f *openAPIA5Fixture) { f.legacy.fail = true; f.v2.fail = true }},
		{name: "admin on user route", status: 200, change: func(f *openAPIA5Fixture) { f.base.a1.users.user.Role = service.RoleAdmin }},
		{name: "backend mode admin on user route", status: 200, change: func(f *openAPIA5Fixture) {
			f.base.a1.users.user.Role = service.RoleAdmin
			f.base.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true"
		}},
		{name: "ignored pagination identity fields", query: "?user_id=42&admin=true&role=admin&page=bad&page_size=1&limit=1&include_admin=true", status: 200},
	}
	heavy := 200
	if v2 {
		heavy = 429
	}
	cases = append(cases, openAPIA5Case{name: "heavy limiter registration", status: heavy, change: func(f *openAPIA5Fixture) {
		f.base.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"public_ip_rpm":0,"exempt_admin":false}`
		f.base.a1.counter.count = 2
	}})
	disabled := 200
	if v2 {
		disabled = 403
	} else if detail {
		disabled = 404
	}
	for _, mode := range []string{"disabled", "wrong mode", "settings failure"} {
		status := disabled
		reason := ""
		if v2 {
			reason = "CHANNEL_MONITOR_DISABLED"
			if mode != "disabled" {
				reason = "CHANNEL_MONITOR_MODE_MISMATCH"
			}
		} else if detail {
			reason = "CHANNEL_MONITOR_NOT_FOUND"
		}
		if mode == "settings failure" && !v2 {
			status = 200
			reason = ""
		}
		cases = append(cases, openAPIA5Case{name: mode, status: status, reason: reason, change: func(f *openAPIA5Fixture) {
			switch mode {
			case "disabled":
				f.base.a1.settings.values[service.SettingKeyChannelMonitorEnabled] = "false"
			case "wrong mode":
				m := "v2"
				if v2 {
					m = "v1"
				}
				f.base.a1.settings.values[service.SettingKeyChannelMonitorMode] = m
			case "settings failure":
				f.base.a1.settings.multipleErr = true
			}
		}, inspect: func(t *testing.T, f *openAPIA5Fixture, b map[string]any) {
			if mode != "settings failure" && !v2 && !detail {
				if len(openAPIA5Array(t, b["items"])) != 0 || f.legacy.calls != 0 {
					t.Fatal("disabled list not empty/no-read")
				}
			}
		}})
	}
	if !v2 {
		if detail {
			for _, id := range []string{"bad", "0", "-1", "9223372036854775808", "1.5"} {
				cases = append(cases, openAPIA5Case{name: "invalid id " + id, id: id, status: 400, reason: "INVALID_MONITOR_ID"})
			}
			for _, id := range []string{"999", "9223372036854775807"} {
				cases = append(cases, openAPIA5Case{name: "missing id " + id, id: id, status: 404, reason: "CHANNEL_MONITOR_NOT_FOUND"})
			}
			for _, id := range []string{"0301", "+301"} {
				cases = append(cases, openAPIA5Case{name: "decimal id " + id, id: id, status: 200})
			}
			cases = append(cases, openAPIA5Case{name: "disabled monitor hidden", status: 404, reason: "CHANNEL_MONITOR_NOT_FOUND", change: func(f *openAPIA5Fixture) { f.legacy.monitor.Enabled = false }}, openAPIA5Case{name: "latest unavailable", status: 500, change: func(f *openAPIA5Fixture) { f.legacy.latestFail = true }}, openAPIA5Case{name: "availability unavailable", status: 500, change: func(f *openAPIA5Fixture) { f.legacy.availabilityFail = true }}, openAPIA5Case{name: "disabled before id parse", id: "bad", status: 404, reason: "CHANNEL_MONITOR_NOT_FOUND", change: func(f *openAPIA5Fixture) {
				f.base.a1.settings.values[service.SettingKeyChannelMonitorEnabled] = "false"
			}})
		} else {
			cases = append(cases, openAPIA5Case{name: "aggregate failures degrade", status: 200, change: func(f *openAPIA5Fixture) { f.legacy.aggregateFail = true }, inspect: func(t *testing.T, f *openAPIA5Fixture, b map[string]any) {
				v := openAPIA5Map(t, openAPIA5Array(t, b["items"])[0])
				if v["primary_status"] != "" || v["primary_latency_ms"] != nil || v["primary_ping_latency_ms"] != nil || v["availability_7d"] != json.Number("0") || len(openAPIA5Array(t, v["timeline"])) != 0 {
					t.Fatal("degraded defaults drift")
				}
			}})
			for _, setting := range []string{"true", "false", "TRUE", ""} {
				cases = append(cases, openAPIA5Case{name: "quota visibility " + setting, status: 200, change: func(f *openAPIA5Fixture) {
					f.base.a1.settings.values[service.SettingKeyChannelMonitorShowQuota] = setting
				}, inspect: func(t *testing.T, _ *openAPIA5Fixture, b map[string]any) {
					v := openAPIA5Map(t, openAPIA5Array(t, b["items"])[0])
					q, ok := v["latest_quota"]
					if ok != (setting == "true") {
						t.Fatal("quota server gate drift")
					}
					if ok {
						m := openAPIA5Map(t, q)
						if m["balance"] != json.Number("9.5") || len(openAPIA5Array(t, m["tiers"])) != 1 || len(openAPIA5Array(t, m["balances"])) != 1 {
							t.Fatal("quota shape drift")
						}
					}
				}})
			}
			cases = append(cases, openAPIA5Case{name: "quota enabled without data", status: 200, change: func(f *openAPIA5Fixture) {
				f.base.a1.settings.values[service.SettingKeyChannelMonitorShowQuota] = "true"
				f.legacy.latest[0].Quota = nil
			}, inspect: func(t *testing.T, _ *openAPIA5Fixture, b map[string]any) {
				if _, ok := openAPIA5Map(t, openAPIA5Array(t, b["items"])[0])["latest_quota"]; ok {
					t.Fatal("nil quota should be omitted")
				}
			}})
		}
	} else {
		for _, kind := range []string{"nil config", "disabled config", "config failure", "group authorization failure", "subscription authorization failure"} {
			status, reason := 500, ""
			if kind == "nil config" || kind == "disabled config" {
				status = 403
				reason = "CHANNEL_MONITOR_DISABLED"
			}
			cases = append(cases, openAPIA5Case{name: kind, status: status, reason: reason, change: func(f *openAPIA5Fixture) {
				switch kind {
				case "nil config":
					f.v2.configNil = true
				case "disabled config":
					f.v2.config.Enabled = false
				case "config failure":
					f.v2.configFail = true
				case "group authorization failure":
					f.base.groups.fail = true
				case "subscription authorization failure":
					f.base.subs.fail = true
				}
			}})
		}
		cases = append(cases, openAPIA5Case{name: "empty allowed group scope", status: 200, change: func(f *openAPIA5Fixture) { f.base.groups.groups = nil }, inspect: func(t *testing.T, f *openAPIA5Fixture, _ map[string]any) {
			if !f.v2.filter.RestrictGroups || len(f.v2.filter.AllowedGroupIDs) != 0 {
				t.Fatal("empty scope broadened")
			}
		}}, openAPIA5Case{name: "foreign requested group remains restricted", query: "?group_id=99", status: 200, inspect: func(t *testing.T, f *openAPIA5Fixture, _ map[string]any) {
			if fmt.Sprint(f.v2.filter.GroupIDs) != "[99]" || fmt.Sprint(f.v2.filter.AllowedGroupIDs) != "[11 13 14]" || !f.v2.filter.RestrictGroups {
				t.Fatal("requested group granted access")
			}
		}})
		for _, rangeValue := range []string{"90m", "24h", "7d", "30d", "%20", "%2024h%20"} {
			cases = append(cases, openAPIA5Case{name: "range " + rangeValue, query: "?range=" + rangeValue, status: 200, inspect: func(t *testing.T, f *openAPIA5Fixture, _ map[string]any) {
				window, bucket := 90*time.Minute, 5*time.Minute
				switch rangeValue {
				case "24h", "%2024h%20":
					window = 24 * time.Hour
					bucket = time.Hour
				case "7d":
					window = 7 * 24 * time.Hour
					bucket = 12 * time.Hour
				case "30d":
					window = 30 * 24 * time.Hour
					bucket = 24 * time.Hour
				}
				v := f.v2.filter
				if v.End.Sub(v.Start) != window || v.Bucket != bucket || !v.End.Equal(v.End.Truncate(bucket)) {
					t.Fatal("range bucket drift")
				}
			}})
		}
		for _, q := range []string{"range=bad", "range=24H", "range=1h"} {
			cases = append(cases, openAPIA5Case{name: q, query: "?" + q, status: 400})
		}
		cases = append(cases, openAPIA5Case{name: "mixed repeated comma filters", query: "?range=24h&range=bad&platform=%20openai%20,Other,,&platform=Other,openai&model=z,a&model=%20a%20,,&group_id=0013,%2B11&group_id=14,13&group_id=%20&user_id=42", status: 200, inspect: func(t *testing.T, f *openAPIA5Fixture, _ map[string]any) {
			v := f.v2.filter
			if fmt.Sprint(v.Platforms) != "[Other openai]" || fmt.Sprint(v.Models) != "[a z]" || fmt.Sprint(v.GroupIDs) != "[11 13 14]" || v.Range != "24h" {
				t.Fatalf("queryList normalization %+v", v)
			}
		}}, openAPIA5Case{name: "empty list tokens", query: "?group_id=,%20,,&group_id=&platform=,,&model=%20", status: 200, inspect: func(t *testing.T, f *openAPIA5Fixture, _ map[string]any) {
			if len(f.v2.filter.GroupIDs)+len(f.v2.filter.Models)+len(f.v2.filter.Platforms) != 0 {
				t.Fatal("empty tokens not ignored")
			}
		}}, openAPIA5Case{name: "max int64 group", query: "?group_id=9223372036854775807", status: 200, inspect: func(t *testing.T, f *openAPIA5Fixture, _ map[string]any) {
			if fmt.Sprint(f.v2.filter.GroupIDs) != "[9223372036854775807]" {
				t.Fatal("int64 precision lost")
			}
		}})
		for _, v := range []string{"0", "-1", "bad", "1.5", "1e2", "9223372036854775808", "-9223372036854775809"} {
			cases = append(cases, openAPIA5Case{name: "bad group token " + v, query: "?group_id=11&group_id=" + v, status: 400, invalidRequest: true})
		}
		if strings.HasSuffix(path, "/matrix") {
			for _, g := range []string{"platform", "platform_group", "platform_model", "platform_group_model", "%20platform%20"} {
				cases = append(cases, openAPIA5Case{name: "group_by " + g, query: "?group_by=" + g, status: 200, inspect: func(t *testing.T, f *openAPIA5Fixture, b map[string]any) {
					want := g
					if g == "%20platform%20" {
						want = "platform"
					}
					if string(f.v2.groupBy) != want || b["group_by"] != want {
						t.Fatal("group_by drift")
					}
				}})
			}
			cases = append(cases, openAPIA5Case{name: "invalid group_by", query: "?group_by=bad", status: 400})
		} else {
			cases = append(cases, openAPIA5Case{name: "group_by ignored", query: "?group_by=bad", status: 200})
		}
		if strings.HasSuffix(path, "/users") {
			cases = append(cases, openAPIA5Case{name: "missing self appended", status: 200, change: func(f *openAPIA5Fixture) { f.v2.missingSelf = true }}, openAPIA5Case{name: "top ten plus self", status: 200, change: func(f *openAPIA5Fixture) { f.v2.manyUsers = true }, inspect: func(t *testing.T, _ *openAPIA5Fixture, b map[string]any) {
				items := openAPIA5Array(t, b["items"])
				if len(items) != 11 || openAPIA5Map(t, items[10])["rank"] != json.Number("13") {
					t.Fatal("top ten plus self projection")
				}
			}}, openAPIA5Case{name: "ranking hidden", status: 200, change: func(f *openAPIA5Fixture) {
				f.base.a1.settings.values[service.SettingKeyChannelMonitorHideUserRanking] = "true"
			}, inspect: func(t *testing.T, f *openAPIA5Fixture, b map[string]any) {
				if len(openAPIA5Array(t, b["items"])) != 0 || f.v2.calls != 0 || f.v2.configCalls != 1 {
					t.Fatal("hidden ranking queried data")
				}
			}})
		}
		if !strings.HasSuffix(path, "/dimensions") && !strings.HasSuffix(path, "/errors") {
			cases = append(cases, openAPIA5Case{name: "throughput visible", status: 200, change: func(f *openAPIA5Fixture) {
				f.base.a1.settings.values[service.SettingKeyChannelMonitorHideThroughput] = "false"
			}}, openAPIA5Case{name: "privacy reader absent", status: 200, change: func(f *openAPIA5Fixture) {
				f.noRuntimeReader = true
				f.base.a1.settings.values[service.SettingKeyChannelMonitorHideThroughput] = "false"
			}})
		}
	}
	if v2 && (strings.HasSuffix(path, "/dimensions") || strings.HasSuffix(path, "/errors")) {
		cases = append(cases, openAPIA5Case{name: "admin shared handler bypasses user group lookup", status: 200, change: func(f *openAPIA5Fixture) {
			f.base.a1.users.user.Role = service.RoleAdmin
			f.base.groups.fail = true
			f.base.subs.fail = true
		}})
	}
	for i := range cases {
		if cases[i].id == "" {
			cases[i].id = "301"
		}
		if cases[i].auth == "" {
			cases[i].auth = "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey)
		}
	}
	return cases
}

func openAPIA5CheckMetrics(t *testing.T, b any, hide bool) {
	t.Helper()
	switch v := b.(type) {
	case []any:
		for _, x := range v {
			openAPIA5CheckMetrics(t, x, hide)
		}
	case map[string]any:
		for key, x := range v {
			if key != "metrics" {
				openAPIA5CheckMetrics(t, x, hide)
				continue
			}
			m := openAPIA5Map(t, x)
			for _, k := range []string{"success_requests", "error_requests", "request_count", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "token_count", "cache_rate_numerator", "cache_rate_denominator"} {
				if m[k] != json.Number("0") {
					t.Fatalf("volume %s not redacted", k)
				}
			}
			for _, k := range []string{"ttft", "duration"} {
				if openAPIA5Map(t, m[k])["sample_count"] != json.Number("0") {
					t.Fatal("sample volume exposed")
				}
			}
			for _, k := range []string{"upstream_affected_requests", "upstream_attempt_count"} {
				if _, ok := m[k]; ok {
					t.Fatal("upstream volume exposed")
				}
			}
			if openAPIA5Map(t, m["ttft"])["p50_ms"] != nil {
				if m["error_rate"] != json.Number("0.1") || m["success_rate"] != json.Number("0.9") || m["cache_rate"] != json.Number("0.5") || openAPIA5Map(t, m["ttft"])["p50_ms"] != json.Number("120") {
					t.Fatal("public ratios or latency changed")
				}
				if !hide && (m["rpm"] != json.Number("2.5") || m["tpm"] != json.Number("12.5")) {
					t.Fatal("configured visible throughput was lost")
				}
			}
			if hide && (m["rpm"] != json.Number("0") || m["tpm"] != json.Number("0")) {
				t.Fatal("throughput exposed")
			}
		}
	}
}
func openAPIA5VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	t.Helper()
	seen := map[string]bool{}
	v2 := stringsContainsV2(path)
	for _, tc := range openAPIA5Cases(t, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA5NewFixture(t, path)
			if tc.change != nil {
				tc.change(f)
			}
			r := f.router(t)
			req := httptest.NewRequest(strings.ToUpper(method), openAPIA2URL(path, tc.id)+tc.query, nil)
			req.RemoteAddr = "203.0.113.9:1234"
			req.Header.Set("X-User-ID", "42")
			req.Header.Set("X-User-Role", "admin")
			if tc.auth != "-" {
				req.Header.Set("Authorization", tc.auth)
			}
			err := openAPIValidateRequest(op, req, map[string]string{"id": tc.id}, nil)
			if (err != nil) != tc.invalidRequest {
				t.Fatalf("request invalid=%v error=%v", tc.invalidRequest, err)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
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
			if tc.status == 429 && rec.Header().Get("Retry-After") != "2" {
				t.Fatal("retry header drift")
			}
			if tc.status == 401 || tc.status == 429 || tc.name == "backend mode" {
				if f.legacy.calls+f.v2.calls+f.v2.configCalls != 0 {
					t.Fatal("auth/limiter rejection reached monitor storage")
				}
			}
			if strings.Contains(rec.Body.String(), "PRIVATE-MONITOR-CANARY") || strings.Contains(rec.Body.String(), "PRIVATE-POLICY-CANARY") || strings.Contains(rec.Body.String(), "OTHER-IDENTITY-CANARY") {
				t.Fatal("private monitor/policy/other identity leaked")
			}
			adminShared := f.base.a1.users.user.Role == service.RoleAdmin && (strings.HasSuffix(path, "/dimensions") || strings.HasSuffix(path, "/errors"))
			if strings.Contains(rec.Body.String(), "ADMIN-DETAIL-CANARY") && !adminShared {
				t.Fatal("non-admin error details leaked")
			}
			if tc.status == 200 {
				b := openAPIA5Map(t, body["data"])
				if v2 {
					if f.v2.calls > 0 {
						if f.v2.filter.RestrictGroups == adminShared {
							t.Fatal("role-specific group scope drift")
						}
						if f.v2.admin != (adminShared && strings.HasSuffix(path, "/errors")) {
							t.Fatal("includeAdmin argument drift")
						}
					}
					hide := tc.name != "throughput visible"
					openAPIA5CheckMetrics(t, b, hide)
					if strings.HasSuffix(path, "/dimensions") {
						for _, k := range []string{"platforms", "groups", "models"} {
							for _, x := range openAPIA5Array(t, b[k]) {
								want := json.Number("0")
								if adminShared {
									want = "100"
								}
								if openAPIA5Map(t, x)["request_count"] != want {
									t.Fatal("role dimension count drift")
								}
							}
						}
					}
					if strings.HasSuffix(path, "/errors") {
						for _, x := range openAPIA5Array(t, b["items"]) {
							m := openAPIA5Map(t, x)
							_, details := m["details"]
							want := json.Number("0")
							if adminShared {
								want = "10"
							}
							if m["count"] != want || details != adminShared || m["rate"] != json.Number("0.1") || m["ignored"] != true {
								t.Fatal("error projection drift")
							}
						}
					}
					if strings.HasSuffix(path, "/snapshot") {
						c := openAPIA5Map(t, b["config"])
						if c["group_ids"] != nil || c["ignored_error_categories"] != nil {
							t.Fatal("policy lists not nulled")
						}
						if _, ok := c["updated_by"]; ok {
							t.Fatal("operator identity exposed")
						}
					}
					if strings.HasSuffix(path, "/users") {
						self := 0
						for _, x := range openAPIA5Array(t, b["items"]) {
							m := openAPIA5Map(t, x)
							if m["is_self"] == true {
								self++
								if m["user_id"] != json.Number("41") || m["can_drilldown"] != true || m["display_label"] != "Me" {
									t.Fatal("self projection drift")
								}
							} else {
								for _, k := range []string{"user_id", "email", "username"} {
									if _, ok := m[k]; ok {
										t.Fatal("other identity exposed")
									}
								}
								if m["can_drilldown"] != false {
									t.Fatal("other drilldown exposed")
								}
							}
						}
						if tc.name != "ranking hidden" && self != 1 {
							t.Fatal("self absent/duplicated")
						}
					}
				} else if tc.name == "populated" {
					if strings.Contains(path, "{id}") {
						if fmt.Sprint(f.legacy.windows) != "[7 15 30]" || len(openAPIA5Array(t, b["models"])) != 2 {
							t.Fatal("detail windows/models drift")
						}
					} else {
						item := openAPIA5Map(t, openAPIA5Array(t, b["items"])[0])
						if len(openAPIA5Array(t, item["extra_models"])) != 1 || len(openAPIA5Array(t, item["timeline"])) != 2 || item["primary_ping_latency_ms"] != json.Number("12") || item["availability_7d"] != json.Number("90") {
							t.Fatal("legacy populated projection drift")
						}
					}
				}
				if tc.inspect != nil {
					tc.inspect(t, f, b)
				}
			}
			seen[fmt.Sprint(tc.status)] = true
		})
	}
	for code := range op.Responses {
		if !seen[code] {
			t.Errorf("response lacks executable case: %s", code)
		}
	}
}
func TestOpenAPIA5Monitors(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, key := range openAPIA5Routes {
		path := strings.TrimPrefix(key, "GET ")
		t.Run(key, func(t *testing.T) { openAPIA5VerifyResponses(t, "get", path, doc.Paths[path]["get"]) })
	}
}
