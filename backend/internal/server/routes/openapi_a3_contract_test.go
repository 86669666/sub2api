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

var openAPIA3Routes = []string{"GET /api/v1/usage", "GET /api/v1/usage/errors", "GET /api/v1/usage/errors/{id}", "GET /api/v1/usage/{id}", "GET /api/v1/usage/stats", "GET /api/v1/usage/dashboard/stats", "GET /api/v1/usage/dashboard/trend", "GET /api/v1/usage/dashboard/models", "GET /api/v1/usage/dashboard/snapshot-v2", "POST /api/v1/usage/dashboard/api-keys-usage", "GET /api/v1/user/api-keys/{id}/usage/daily"}

func openAPIA3Operation(key string) bool {
	for _, v := range openAPIA3Routes {
		if key == v {
			return true
		}
	}
	return false
}

type openAPIA3Case struct {
	name, id, query, body, auth string
	status                      int
	change                      func(*openAPIA3Fixture)
	inspect                     func(*testing.T, *openAPIA3Fixture, map[string]any)
	requestInvalid              bool
}

func openAPIA3Cases(t *testing.T, path string) []openAPIA3Case {
	cases := []openAPIA3Case{
		{name: "populated", status: 200},
		{name: "empty nullable response", status: 200, change: func(f *openAPIA3Fixture) {
			f.usage.empty = true
			f.ops.empty = true
			f.usage.record = service.UsageLog{ID: 101, UserID: 41, CreatedAt: time.Date(2035, 1, 2, 0, 0, 0, 0, time.UTC)}
		}},
		{name: "missing auth", auth: "-", status: 401},
		{name: "invalid token", auth: "Bearer invalid", status: 401},
		{name: "wrong scheme", auth: "Basic invalid", status: 401},
		{name: "expired token", auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey), status: 401},
		{name: "revoked token", status: 401, change: func(f *openAPIA3Fixture) { f.base.a1.users.user.TokenVersion++ }},
		{name: "backend mode", status: 403, change: func(f *openAPIA3Fixture) { f.base.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }},
		{name: "auth store error", status: 500, change: func(f *openAPIA3Fixture) { f.base.a1.users.lookupErr = fmt.Errorf("synthetic auth unavailable") }},
		{name: "global limiter", status: 429, change: func(f *openAPIA3Fixture) { f.base.limit() }},
		{name: "heavy limiter", status: 429, change: func(f *openAPIA3Fixture) {
			f.base.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":100,"heavy_rpm":1,"public_ip_rpm":0,"exempt_admin":false}`
			f.base.a1.counter.count = 2
		}},
		{name: "limiter fails open", status: 200, change: func(f *openAPIA3Fixture) { f.base.limit(); f.base.a1.counter.fail = true }},
		{name: "repository failure", status: 500, change: func(f *openAPIA3Fixture) { f.usage.fail = true; f.ops.fail = true }},
	}
	if strings.Contains(path, "{id}") {
		other := 403
		if strings.Contains(path, "/errors/") {
			other = 404
		}
		cases = append(cases, openAPIA3Case{name: "other owner", id: "102", status: other}, openAPIA3Case{name: "missing row", id: "999", status: 404}, openAPIA3Case{name: "malformed id", id: "bad", status: 400}, openAPIA3Case{name: "overflow id", id: "9223372036854775808", status: 400})
	}
	filtered := path == "/api/v1/usage" || path == "/api/v1/usage/stats" || path == "/api/v1/usage/dashboard/trend" || path == "/api/v1/usage/dashboard/models" || path == "/api/v1/usage/dashboard/snapshot-v2"
	if filtered {
		for _, q := range []string{"api_key_id=bad", "api_key_id=9223372036854775808", "group_id=9223372036854775808", "request_type=2", "request_type=bad", "stream=bad", "native_compaction_v2=bad", "billing_type=128", "billing_type=-129", "billing_mode=all", "start_date=bad", "end_date=2035-02-30"} {
			cases = append(cases, openAPIA3Case{name: "invalid query " + q, query: "?" + q, status: 400})
		}
		cases = append(cases, openAPIA3Case{name: "other key", query: "?api_key_id=102", status: 403}, openAPIA3Case{name: "missing key", query: "?api_key_id=999", status: 404}, openAPIA3Case{name: "full scoped filters", status: 200, query: "?api_key_id=101&group_id=-11&model=%20requested%20&request_type=STREAM&stream=bad&native_compaction_v2=1&billing_type=-128&billing_mode=video&start_date=2035-01-02&end_date=2035-01-03&timezone=Asia%2FShanghai", inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
			v := f.usage.filters
			if v.UserID != 41 || v.APIKeyID != 101 || v.AccountID != 0 || v.GroupID != -11 || v.Model != "requested" || v.ModelFilterSource != "requested" || v.RequestType == nil || *v.RequestType != int16(service.RequestTypeStream) || v.Stream != nil || v.NativeCompactionV2 == nil || !*v.NativeCompactionV2 || v.BillingType == nil || *v.BillingType != -128 || v.BillingMode != "video" || v.StartTime == nil || v.EndTime == nil {
				t.Fatalf("filter propagation %+v", v)
			}
			if v.StartTime.Format(time.RFC3339) != "2035-01-02T00:00:00+08:00" || v.EndTime.Format(time.RFC3339) != "2035-01-04T00:00:00+08:00" {
				t.Fatal("calendar range changed")
			}
		}}, openAPIA3Case{name: "stream and int8 boundary", query: "?stream=True&billing_type=127&group_id=-9223372036854775808&native_compaction_v2=false", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
			v := f.usage.filters
			if v.Stream == nil || !*v.Stream || v.RequestType != nil || v.BillingType == nil || *v.BillingType != 127 || v.GroupID != -9223372036854775808 {
				t.Fatal("boundary filter drift")
			}
		}}, openAPIA3Case{name: "reverse dates forwarded", query: "?start_date=2035-01-03&end_date=2035-01-01&timezone=InvalidZone", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
			if !f.usage.filters.StartTime.After(*f.usage.filters.EndTime) {
				t.Fatal("reverse range unexpectedly changed")
			}
		}})
	}
	if path == "/api/v1/usage" {
		cases = append(cases, openAPIA3Case{name: "pagination fallbacks", query: "?page=-1&page_size=1001&limit=5&sort_by=cost&sort_order=asc", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
			p := f.usage.params
			if p.Page != 1 || p.PageSize != 20 || p.SortBy != "cost" || p.SortOrder != "asc" || f.usage.filters.StartTime != nil || f.usage.filters.EndTime != nil {
				t.Fatal("pagination/default date drift")
			}
		}}, openAPIA3Case{name: "limit alias", query: "?page=2&limit=5", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
			if f.usage.params.Page != 2 || f.usage.params.PageSize != 5 {
				t.Fatal("pagination alias drift")
			}
		}})
	}
	if filtered && path != "/api/v1/usage" {
		for _, period := range []string{"today", "week", "month", "year"} {
			cases = append(cases, openAPIA3Case{name: "period " + period, query: "?period=" + period + "&timezone=UTC", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
				v := f.usage.filters
				if v.StartTime == nil || v.EndTime == nil || !v.StartTime.Before(*v.EndTime) || time.Since(*v.EndTime) > time.Minute {
					t.Fatal("period range drift")
				}
			}})
		}
	}
	if path == "/api/v1/usage/dashboard/trend" {
		cases = append(cases, openAPIA3Case{name: "granularity passed through", query: "?granularity=arbitrary", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
			if f.usage.granularity != "arbitrary" || b["granularity"] != "arbitrary" {
				t.Fatal("trend normalization invented")
			}
		}})
	}
	if path == "/api/v1/usage/dashboard/models" {
		cases = append(cases, openAPIA3Case{name: "upstream source forbidden", query: "?model_source=upstream", status: 400})
	}
	if path == "/api/v1/usage/dashboard/snapshot-v2" {
		cases = append(cases, openAPIA3Case{name: "include all sections", query: "?include_group_stats=true&granularity=hour", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
			if f.usage.calls != 3 || b["groups"] == nil || b["granularity"] != "hour" {
				t.Fatal("snapshot groups omitted")
			}
		}}, openAPIA3Case{name: "omit all sections", query: "?include_trend=false&include_model_stats=false&include_group_stats=false&granularity=bogus", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
			if f.usage.calls != 0 || len(b) != 4 || b["granularity"] != "day" {
				t.Fatal("snapshot optional sections drift")
			}
		}})
		for _, v := range []string{"include_trend=bad", "include_model_stats=bad", "include_group_stats=%20true%20"} {
			cases = append(cases, openAPIA3Case{name: v, query: "?" + v, status: 400})
		}
	}
	if strings.HasPrefix(path, "/api/v1/usage/errors") {
		cases = append(cases, openAPIA3Case{name: "error view disabled", status: 403, change: func(f *openAPIA3Fixture) {
			f.base.a1.settings.values[service.SettingKeyAllowUserViewErrorRequests] = "false"
		}}, openAPIA3Case{name: "ops unavailable", status: 503, change: func(f *openAPIA3Fixture) { f.noOps = true }})
		if strings.Contains(path, "{id}") {
			cases = append(cases, openAPIA3Case{name: "unowned error hidden", id: "103", status: 404}, openAPIA3Case{name: "nonpositive error", id: "0", status: 400})
		} else {
			for _, q := range []string{"api_key_id=-1", "api_key_id=9223372036854775808", "status_code=-1", "status_code=9223372036854775808", "start_date=bad", "end_date=bad"} {
				cases = append(cases, openAPIA3Case{name: q, query: "?" + q, status: 400})
			}
			cases = append(cases, openAPIA3Case{name: "error filters and foreign key", query: "?api_key_id=102&user_id=42&account_id=7&model=%20requested%20&status_code=502&category=upstream&page=2&page_size=500&start_date=2035-01-02&end_date=2035-01-03&timezone=UTC", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
				v := f.ops.filter
				if v.UserID == nil || *v.UserID != 41 || v.APIKeyID == nil || *v.APIKeyID != 102 || v.AccountID != nil || v.PageSize != 100 || v.Page != 2 || v.Model != "requested" || !v.ModelFuzzy || !v.ExcludeCountTokens || v.IncludeRecoveredUpstream || v.View != "all" || len(v.ErrorPhasesAny) != 3 || len(v.StatusCodes) != 1 || v.StatusCodes[0] != 502 || v.EndTime.Format(time.RFC3339) != "2035-01-04T00:00:00Z" {
					t.Fatalf("error filter drift %+v", v)
				}
				if len(openAPIA1As[[]any](t, b["items"])) != 0 || f.keys.reads != 0 {
					t.Fatal("error key filtering should use combined repository scope")
				}
			}}, openAPIA3Case{name: "unknown category ignored", query: "?category=other&api_key_id=0", status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, _ map[string]any) {
				if len(f.ops.filter.ErrorTypesAny) != 0 || len(f.ops.filter.ErrorPhasesAny) != 0 || f.ops.filter.APIKeyID != nil {
					t.Fatal("category/zero behavior changed")
				}
			}})
		}
	}
	if strings.Contains(path, "/usage/daily") {
		for _, days := range []string{"0", "91", "bad", "1.0", "9223372036854775808", "%201%20"} {
			cases = append(cases, openAPIA3Case{name: "days " + days, query: "?days=" + days, status: 400})
		}
		for _, days := range []int{1, 90} {
			cases = append(cases, openAPIA3Case{name: fmt.Sprint("valid days ", days), query: fmt.Sprintf("?days=%d&timezone=UTC", days), status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
				if f.usage.userID != 41 || f.usage.keyID != 101 || f.usage.granularity != "day" || f.usage.end.Sub(f.usage.start) != time.Duration(days)*24*time.Hour || b["days"] != json.Number(fmt.Sprint(days)) {
					t.Fatal("daily scope/range drift")
				}
				items := openAPIA1As[[]any](t, b["items"])
				if openAPIA1As[map[string]any](t, items[0])["cache_write_tokens"] != json.Number("5") {
					t.Fatal("cache token mapping")
				}
			}})
		}
	}
	if strings.HasSuffix(path, "api-keys-usage") {
		for _, body := range []string{`{}`, `{"api_key_ids":null}`, `{"api_key_ids":"101"}`, `{"api_key_ids":[101.0]}`, `{"api_key_ids":[101e0]}`, `{"api_key_ids":[9223372036854775808]}`, `{"api_key_ids":[-9223372036854775809]}`, `{"api_key_ids":[true]}`, `{"api_key_ids":[` + strings.Repeat("101,", 100) + `101]}`, `{`} {
			cases = append(cases, openAPIA3Case{name: "invalid body " + body, body: body, status: 400, requestInvalid: true})
		}
		cases = append(cases, openAPIA3Case{name: "mixed batch scope", body: `{"api_key_ids":[101,102,999,null],"future":11.0}`, status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
			if f.keys.verifyUser != 41 || fmt.Sprint(f.keys.verifyIDs) != "[101 102 999 0]" || fmt.Sprint(f.usage.batch) != "[101]" || !f.usage.start.IsZero() || !f.usage.end.IsZero() {
				t.Fatal("batch scoping drift")
			}
			if len(openAPIA1As[map[string]any](t, b["stats"])) != 1 {
				t.Fatal("foreign key in batch")
			}
		}}, openAPIA3Case{name: "all foreign unknown", body: `{"api_key_ids":[102,999,-9223372036854775808,9223372036854775807]}`, status: 200, inspect: func(t *testing.T, f *openAPIA3Fixture, b map[string]any) {
			if f.usage.calls != 0 || len(openAPIA1As[map[string]any](t, b["stats"])) != 0 {
				t.Fatal("all invalid batch queried stats")
			}
		}}, openAPIA3Case{name: "empty batch", body: `{"api_key_ids":[]}`, status: 200}, openAPIA3Case{name: "100 ids accepted", body: `{"api_key_ids":[` + strings.Repeat("101,", 99) + `101]}`, status: 200}, openAPIA3Case{name: "ownership store failure", status: 500, change: func(f *openAPIA3Fixture) { f.keys.fail = true }})
	}
	for j := range cases {
		if cases[j].id == "" {
			cases[j].id = "101"
		}
		if cases[j].auth == "" {
			cases[j].auth = "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey)
		}
		if cases[j].body == "" {
			cases[j].body = `{"api_key_ids":[101]}`
		}
	}
	return cases
}
func openAPIA3VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range openAPIA3Cases(t, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA3NewFixture(t)
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
			err := openAPIValidateRequest(op, req, map[string]string{"id": tc.id}, []byte(tc.body))
			if tc.requestInvalid {
				if err == nil {
					t.Fatal("invalid request accepted by contract")
				}
			} else if err != nil {
				t.Fatalf("request validation: %v", err)
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
				t.Fatal("admin-only canary in response")
			}
			if tc.status == 401 || tc.name == "backend mode" || tc.status == 429 {
				if f.usage.calls+f.ops.calls+f.keys.reads != 0 {
					t.Fatal("rejected auth/limiter reached business repository")
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
				tc.inspect(t, f, openAPIA1As[map[string]any](t, body["data"]))
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
func TestOpenAPIA3Usage(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, key := range openAPIA3Routes {
		parts := strings.SplitN(key, " ", 2)
		t.Run(key, func(t *testing.T) {
			openAPIA3VerifyResponses(t, strings.ToLower(parts[0]), parts[1], doc.Paths[parts[1]][strings.ToLower(parts[0])])
		})
	}
}
