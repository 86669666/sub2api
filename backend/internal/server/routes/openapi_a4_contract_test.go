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

var openAPIA4Routes = []string{"GET /api/v1/subscriptions", "GET /api/v1/subscriptions/active", "GET /api/v1/subscriptions/progress", "GET /api/v1/subscriptions/summary"}

func openAPIA4Operation(key string) bool {
	for _, v := range openAPIA4Routes {
		if key == v {
			return true
		}
	}
	return false
}

type openAPIA4Case struct {
	name, auth, lang, query string
	status                  int
	change                  func(*openAPIA4Fixture)
	inspect                 func(*testing.T, *openAPIA4Fixture, any)
}

func openAPIA4Items(t *testing.T, path string, data any) []any {
	t.Helper()
	if strings.HasSuffix(path, "/summary") {
		return openAPIA1As[[]any](t, openAPIA1As[map[string]any](t, data)["subscriptions"])
	}
	return openAPIA1As[[]any](t, data)
}
func openAPIA4Sub(t *testing.T, path string, item any) map[string]any {
	t.Helper()
	v := openAPIA1As[map[string]any](t, item)
	if strings.HasSuffix(path, "/progress") {
		return openAPIA1As[map[string]any](t, v["subscription"])
	}
	return v
}
func openAPIA4Count(want int, path string) func(*testing.T, *openAPIA4Fixture, any) {
	return func(t *testing.T, _ *openAPIA4Fixture, data any) {
		items := openAPIA4Items(t, path, data)
		if len(items) != want {
			t.Fatalf("items=%d want=%d", len(items), want)
		}
		if strings.HasSuffix(path, "/summary") && openAPIA1As[map[string]any](t, data)["active_count"] != json.Number(fmt.Sprint(want)) {
			t.Fatal("summary count drift")
		}
	}
}
func openAPIA4Cases(t *testing.T, path string) []openAPIA4Case {
	t.Helper()
	cases := []openAPIA4Case{
		{name: "populated", status: 200, inspect: openAPIA4Count(1, path)},
		{name: "nil list becomes empty array", status: 200, change: func(f *openAPIA4Fixture) { f.subs.rows = nil }, inspect: func(t *testing.T, f *openAPIA4Fixture, data any) {
			openAPIA4Count(0, path)(t, f, data)
			if strings.HasSuffix(path, "/summary") && openAPIA1As[map[string]any](t, data)["total_used_usd"] != json.Number("0") {
				t.Fatal("empty sum drift")
			}
		}},
		{name: "missing auth", auth: "-", status: 401},
		{name: "invalid token", auth: "Bearer invalid", status: 401},
		{name: "wrong scheme", auth: "Basic invalid", status: 401},
		{name: "empty bearer", auth: "Bearer ", status: 401},
		{name: "expired token", auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey), status: 401},
		{name: "revoked token", status: 401, change: func(f *openAPIA4Fixture) { f.base.a1.users.user.TokenVersion++ }},
		{name: "inactive user", status: 401, change: func(f *openAPIA4Fixture) { f.base.a1.users.user.Status = "disabled" }},
		{name: "backend mode", status: 403, change: func(f *openAPIA4Fixture) { f.base.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
		{name: "backend mode admin", status: 200, change: func(f *openAPIA4Fixture) {
			f.base.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true"
			f.base.a1.users.user.Role = service.RoleAdmin
		}},
		{name: "auth store failure", status: 500, change: func(f *openAPIA4Fixture) { f.base.a1.users.lookupErr = fmt.Errorf("synthetic auth store error") }},
		{name: "global limiter", status: 429, change: func(f *openAPIA4Fixture) { f.base.limit() }},
		{name: "limiter fails open", status: 200, change: func(f *openAPIA4Fixture) { f.base.limit(); f.base.a1.counter.fail = true }},
		{name: "heavy limiter does not apply", status: 200, change: func(f *openAPIA4Fixture) {
			f.base.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"public_ip_rpm":0,"exempt_admin":false}`
			f.base.a1.counter.count = 2
		}},
		{name: "list store failure", status: 500, change: func(f *openAPIA4Fixture) { f.subs.listErr = true }},
		{name: "query and spoof scope ignored", status: 200, query: "?user_id=42&group_id=999&page=bad&page_size=1&limit=1&status=suspended", change: func(f *openAPIA4Fixture) { v := f.subs.rows[0]; v.ID = 203; f.subs.rows = append(f.subs.rows, v) }, inspect: openAPIA4Count(2, path)},
	}
	for _, lang := range []string{"en", "zz-ZZ", ""} {
		cases = append(cases, openAPIA4Case{name: "localization " + lang, lang: lang, status: 200, inspect: func(t *testing.T, _ *openAPIA4Fixture, data any) {
			name, desc := "Original", "Original description"
			if lang == "en" {
				name = "Translated"
				desc = "Translated description"
			}
			item := openAPIA4Items(t, path, data)[0]
			v := openAPIA4Sub(t, path, item)
			if strings.HasSuffix(path, "/summary") {
				if v["group_name"] != name {
					t.Fatal("summary localization drift")
				}
				return
			}
			g := openAPIA1As[map[string]any](t, v["group"])
			if g["name"] != name || g["description"] != desc {
				t.Fatal("group localization drift")
			}
			if _, ok := g["i18n"]; ok {
				t.Fatal("translation map exposed")
			}
			if strings.HasSuffix(path, "/progress") && openAPIA1As[map[string]any](t, openAPIA1As[map[string]any](t, item)["progress"])["group_name"] != name {
				t.Fatal("progress localization drift")
			}
		}})
	}
	cases = append(cases, openAPIA4Case{name: "inactive and expired selection", status: 200, change: func(f *openAPIA4Fixture) {
		v := f.subs.rows[0]
		v.ID = 203
		v.ExpiresAt = time.Now().Add(-time.Hour)
		f.subs.rows = append(f.subs.rows, v)
		v.ID = 204
		v.Status = service.SubscriptionStatusSuspended
		v.ExpiresAt = time.Now().Add(time.Hour)
		f.subs.rows = append(f.subs.rows, v)
	}, inspect: func(t *testing.T, f *openAPIA4Fixture, data any) {
		count := 1
		if path == "/api/v1/subscriptions" {
			count = 3
		}
		openAPIA4Count(count, path)(t, f, data)
		if count == 3 {
			items := openAPIA4Items(t, path, data)
			if openAPIA4Sub(t, path, items[1])["status"] != service.SubscriptionStatusExpired || openAPIA4Sub(t, path, items[2])["status"] != service.SubscriptionStatusSuspended {
				t.Fatal("display status normalization drift")
			}
			if f.subs.rows[2].Status != service.SubscriptionStatusActive {
				t.Fatal("display mutated repository")
			}
		}
	}})
	cases = append(cases, openAPIA4Case{name: "expired windows normalize display only", status: 200, change: func(f *openAPIA4Fixture) {
		old := time.Now().Add(-40 * 24 * time.Hour)
		v := &f.subs.rows[0]
		v.StartsAt = old
		v.DailyWindowStart = &old
		v.WeeklyWindowStart = &old
		v.MonthlyWindowStart = &old
	}, inspect: func(t *testing.T, f *openAPIA4Fixture, data any) {
		item := openAPIA4Items(t, path, data)[0]
		v := openAPIA4Sub(t, path, item)
		if strings.HasSuffix(path, "/summary") {
			for _, k := range []string{"daily_used_usd", "weekly_used_usd", "monthly_used_usd"} {
				if _, ok := v[k]; ok {
					t.Fatalf("zero field %s not omitted", k)
				}
			}
			if openAPIA1As[map[string]any](t, data)["total_used_usd"] != json.Number("0") {
				t.Fatal("summary not normalized")
			}
		} else {
			for _, w := range []string{"daily", "weekly", "monthly"} {
				if v[w+"_window_start"] != nil || v[w+"_usage_usd"] != json.Number("0") {
					t.Fatal("expired window display drift")
				}
			}
		}
		if f.subs.rows[0].MonthlyUsageUSD != 10 || f.subs.rows[0].MonthlyWindowStart == nil {
			t.Fatal("read normalized storage")
		}
		if strings.HasSuffix(path, "/progress") {
			p := openAPIA1As[map[string]any](t, openAPIA1As[map[string]any](t, item)["progress"])
			for _, w := range []string{"daily", "weekly", "monthly"} {
				win := openAPIA1As[map[string]any](t, p[w])
				if win["resets_in_seconds"] != json.Number("0") {
					t.Fatal("past reset not clamped")
				}
			}
			if openAPIA1As[map[string]any](t, p["monthly"])["used_usd"] != json.Number("10") {
				t.Fatal("progress must reflect per-item reread")
			}
		}
	}})
	cases = append(cases, openAPIA4Case{name: "missing group relation", status: 200, lang: "en", change: func(f *openAPIA4Fixture) { f.subs.rows[0].Group = nil }, inspect: func(t *testing.T, f *openAPIA4Fixture, data any) {
		item := openAPIA4Items(t, path, data)[0]
		v := openAPIA4Sub(t, path, item)
		if _, ok := v["group"]; ok {
			t.Fatal("nil group not omitted")
		}
		if strings.HasSuffix(path, "/summary") {
			if v["group_name"] != "" {
				t.Fatal("missing group name")
			}
			for _, w := range []string{"daily", "weekly", "monthly"} {
				if _, ok := v[w+"_limit_usd"]; ok {
					t.Fatal("missing group limit present")
				}
			}
		}
		if strings.HasSuffix(path, "/progress") {
			if fmt.Sprint(f.groups.ids) != "[13]" {
				t.Fatal("missing fallback lookup")
			}
			if openAPIA1As[map[string]any](t, openAPIA1As[map[string]any](t, item)["progress"])["group_name"] != "Original" {
				t.Fatal("fallback-only group unexpectedly localized")
			}
		} else if len(f.groups.ids) != 0 {
			t.Fatal("unexpected group lookup")
		}
	}})
	if strings.HasSuffix(path, "/progress") {
		for _, kind := range []string{"subscription read", "group read"} {
			cases = append(cases, openAPIA4Case{name: "partial skip " + kind, status: 200, change: func(f *openAPIA4Fixture) {
				v := f.subs.rows[0]
				v.ID = 203
				f.subs.rows = append(f.subs.rows, v)
				if kind == "subscription read" {
					f.subs.getErrors[201] = true
				} else {
					f.subs.rows[0].Group = nil
					f.groups.fail = true
				}
			}, inspect: func(t *testing.T, f *openAPIA4Fixture, data any) {
				openAPIA4Count(1, path)(t, f, data)
				if openAPIA4Sub(t, path, openAPIA4Items(t, path, data)[0])["id"] != json.Number("203") {
					t.Fatal("failed item not skipped")
				}
			}})
		}
		cases = append(cases, openAPIA4Case{name: "all progress reads skipped", status: 200, change: func(f *openAPIA4Fixture) { f.subs.getErrors[201] = true }, inspect: openAPIA4Count(0, path)}, openAPIA4Case{name: "clamped progress", status: 200, change: func(f *openAPIA4Fixture) {
			f.subs.rows[0].DailyUsageUSD = 25
			f.subs.rows[0].WeeklyUsageUSD = 50
			f.subs.rows[0].MonthlyUsageUSD = 100
		}, inspect: func(t *testing.T, _ *openAPIA4Fixture, data any) {
			p := openAPIA1As[map[string]any](t, openAPIA1As[map[string]any](t, openAPIA4Items(t, path, data)[0])["progress"])
			if p["expires_in_days"] != json.Number("10") {
				t.Fatal("ceil days drift")
			}
			for _, w := range []string{"daily", "weekly", "monthly"} {
				v := openAPIA1As[map[string]any](t, p[w])
				if v["remaining_usd"] != json.Number("0") || v["percentage"] != json.Number("100") {
					t.Fatal("overlimit clamp drift")
				}
			}
		}})
		for _, kind := range []string{"nil limits", "zero limits", "negative limits", "nil windows"} {
			cases = append(cases, openAPIA4Case{name: "omitted windows " + kind, status: 200, change: func(f *openAPIA4Fixture) {
				v := &f.subs.rows[0]
				g := *v.Group
				v.Group = &g
				switch kind {
				case "nil limits":
					g.DailyLimitUSD = nil
					g.WeeklyLimitUSD = nil
					g.MonthlyLimitUSD = nil
				case "zero limits", "negative limits":
					n := 0.0
					if kind == "negative limits" {
						n = -1
					}
					g.DailyLimitUSD = &n
					g.WeeklyLimitUSD = &n
					g.MonthlyLimitUSD = &n
				case "nil windows":
					v.DailyWindowStart = nil
					v.WeeklyWindowStart = nil
					v.MonthlyWindowStart = nil
				}
			}, inspect: func(t *testing.T, _ *openAPIA4Fixture, data any) {
				p := openAPIA1As[map[string]any](t, openAPIA1As[map[string]any](t, openAPIA4Items(t, path, data)[0])["progress"])
				for _, w := range []string{"daily", "weekly", "monthly"} {
					if _, ok := p[w]; ok {
						t.Fatal("disabled window must be omitted")
					}
				}
			}})
		}
	} else if strings.HasSuffix(path, "/summary") {
		cases = append(cases, openAPIA4Case{name: "summary exact totals", status: 200, change: func(f *openAPIA4Fixture) {
			v := f.subs.rows[0]
			v.ID = 203
			v.MonthlyUsageUSD = 7.5
			f.subs.rows = append(f.subs.rows, v)
		}, inspect: func(t *testing.T, f *openAPIA4Fixture, data any) {
			openAPIA4Count(2, path)(t, f, data)
			m := openAPIA1As[map[string]any](t, data)
			if m["total_used_usd"] != json.Number("17.5") {
				t.Fatal("monthly sum drift")
			}
			item := openAPIA4Sub(t, path, openAPIA4Items(t, path, data)[0])
			for k, want := range map[string]json.Number{"daily_used_usd": "2.5", "weekly_used_usd": "5", "monthly_used_usd": "10", "daily_limit_usd": "10", "weekly_limit_usd": "20", "monthly_limit_usd": "40", "group_id": "13"} {
				if item[k] != want {
					t.Fatalf("summary field %s", k)
				}
			}
			if item["expires_at"] != f.subs.rows[0].ExpiresAt.Format(time.RFC3339) {
				t.Fatal("summary expiry format")
			}
		}}, openAPIA4Case{name: "summary zero omissions", status: 200, change: func(f *openAPIA4Fixture) {
			v := &f.subs.rows[0]
			v.DailyUsageUSD = 0
			v.WeeklyUsageUSD = 0
			v.MonthlyUsageUSD = 0
			g := *v.Group
			n := 0.0
			g.DailyLimitUSD = &n
			g.WeeklyLimitUSD = nil
			g.MonthlyLimitUSD = &n
			v.Group = &g
		}, inspect: func(t *testing.T, _ *openAPIA4Fixture, data any) {
			v := openAPIA4Sub(t, path, openAPIA4Items(t, path, data)[0])
			for _, w := range []string{"daily", "weekly", "monthly"} {
				for _, suffix := range []string{"_used_usd", "_limit_usd"} {
					if _, ok := v[w+suffix]; ok {
						t.Fatal("zero/nil field not omitted")
					}
				}
			}
		}})
	} else {
		cases = append(cases, openAPIA4Case{name: "optional relations and revoked projection", status: 200, change: func(f *openAPIA4Fixture) {
			v := &f.subs.rows[0]
			now := time.Now().UTC()
			v.DeletedAt = &now
			v.User = f.base.a1.users.user
			v.AssignedByUser = &service.User{Username: "INTERNAL-CANARY"}
		}, inspect: func(t *testing.T, _ *openAPIA4Fixture, data any) {
			v := openAPIA4Sub(t, path, openAPIA4Items(t, path, data)[0])
			if v["revoked_at"] == nil || v["user"] == nil {
				t.Fatal("optional relations omitted")
			}
		}})
	}
	for i := range cases {
		if cases[i].auth == "" {
			cases[i].auth = "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey)
		}
	}
	return cases
}
func openAPIA4VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range openAPIA4Cases(t, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA4NewFixture(t)
			if tc.change != nil {
				tc.change(f)
			}
			r := f.router(t)
			req := httptest.NewRequest(strings.ToUpper(method), path+tc.query, nil)
			req.RemoteAddr = "203.0.113.9:1234"
			req.Header.Set("Accept-Language", tc.lang)
			req.Header.Set("X-User-ID", "42")
			if tc.auth != "-" {
				req.Header.Set("Authorization", tc.auth)
			}
			if err := openAPIValidateRequest(op, req, nil, nil); err != nil {
				t.Fatal(err)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatalf("response contract: %v body=%s", err, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "INTERNAL-CANARY") {
				t.Fatal("private field exposed")
			}
			if tc.status == 200 || tc.name == "list store failure" {
				if f.subs.listedUser != 41 || f.subs.listCalls+f.subs.activeCalls != 1 {
					t.Fatal("authenticated scope/calls drift")
				}
				if (path == "/api/v1/subscriptions") != (f.subs.listCalls == 1) {
					t.Fatal("wrong repository method")
				}
			} else if f.subs.listCalls+f.subs.activeCalls+len(f.subs.gets)+len(f.groups.ids) != 0 {
				t.Fatal("rejected request reached business storage")
			}
			if !strings.HasSuffix(path, "/progress") && len(f.subs.gets) != 0 {
				t.Fatal("unexpected per-item reread")
			}
			for _, id := range f.subs.gets {
				if id == 202 {
					t.Fatal("foreign subscription progress lookup")
				}
			}
			if tc.status == 429 && rec.Header().Get("Retry-After") != "2" {
				t.Fatal("retry header drift")
			}
			var body map[string]any
			if err := openAPIDecode(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.inspect != nil {
				tc.inspect(t, f, body["data"])
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
func TestOpenAPIA4Subscriptions(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, key := range openAPIA4Routes {
		path := strings.TrimPrefix(key, "GET ")
		t.Run(key, func(t *testing.T) { openAPIA4VerifyResponses(t, "get", path, doc.Paths[path]["get"]) })
	}
}
