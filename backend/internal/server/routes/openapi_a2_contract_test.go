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

type openAPIA2Case struct {
	name, body, id, query, auth, reason string
	headers                             map[string]string
	status                              int
	change                              func(*openAPIA2Fixture)
	inspect                             func(*testing.T, *openAPIA2Fixture, map[string]any)
	requestInvalid                      bool
}

func openAPIA2Cases(t *testing.T, method, path string) []openAPIA2Case {
	token := openAPIA1Token(t, func(c *service.JWTClaims) { c.Role = service.RoleUser }, openAPIA1SigningKey)
	bearer := "Bearer " + token
	validBody := ""
	if method == "post" {
		validBody = `{"name":"synthetic","custom_key":"` + openAPIA2CreateKey + `"}`
	} else if method == "put" {
		validBody = `{"name":"updated"}`
	}
	cases := []openAPIA2Case{
		{name: "real handler owner success", status: 200},
		{name: "missing auth", auth: "-", status: 401},
		{name: "API key is not session JWT", auth: "-", headers: map[string]string{"x-api-key": openAPIA2OwnKey}, status: 401},
		{name: "wrong scheme", auth: "Basic bad", status: 401},
		{name: "empty bearer", auth: "Bearer ", status: 401},
		{name: "invalid signature", auth: "Bearer " + openAPIA1Token(t, nil, "wrong-fixture-signing-secret"), status: 401},
		{name: "expired", auth: "Bearer " + openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey), status: 401},
		{name: "revoked token version", change: func(f *openAPIA2Fixture) { f.a1.users.user.TokenVersion++ }, status: 401},
		{name: "inactive user", change: func(f *openAPIA2Fixture) { f.a1.users.user.Status = service.StatusDisabled }, status: 401},
		{name: "missing user", change: func(f *openAPIA2Fixture) { f.a1.users.lookupErr = service.ErrUserNotFound }, status: 401},
		{name: "auth store unavailable", change: func(f *openAPIA2Fixture) { f.a1.users.lookupErr = fmt.Errorf("synthetic user read failure") }, status: 500},
		{name: "backend mode blocks user", change: func(f *openAPIA2Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }, status: 403},
		{name: "database role overrides claim and bypasses backend mode", change: func(f *openAPIA2Fixture) {
			f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true"
			f.a1.users.user.Role = service.RoleAdmin
		}, status: 200},
		{name: "admin claim cannot elevate database user", auth: "Bearer " + openAPIA1Token(t, nil, openAPIA1SigningKey), change: func(f *openAPIA2Fixture) { f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true" }, status: 403},
		{name: "real panel limiter", change: func(f *openAPIA2Fixture) { f.limit() }, status: 429},
		{name: "panel counter failure fail open", change: func(f *openAPIA2Fixture) { f.limit(); f.a1.counter.fail = true }, status: 200},
		{name: "repository failure", change: openAPIA2RepoFailure, status: 500},
	}
	bound := openAPIA1Token(t, func(c *service.JWTClaims) {
		c.BindingHash = (&service.SessionBinding{IP: "203.0.113.9", UserAgent: "openapi-fixture"}).Hash()
	}, openAPIA1SigningKey)
	cases = append(cases, openAPIA2Case{name: "bound session matches", auth: "Bearer " + bound, change: func(f *openAPIA2Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }, status: 200}, openAPIA2Case{name: "bound session mismatch revokes family", auth: "Bearer " + bound, headers: map[string]string{"User-Agent": "different"}, change: func(f *openAPIA2Fixture) { f.a1.settings.values[service.SettingKeySessionBindingEnabled] = "true" }, status: 401, inspect: func(t *testing.T, f *openAPIA2Fixture, _ map[string]any) {
		if len(f.a1.refresh.revoked) != 1 {
			t.Fatal("session family not revoked")
		}
	}})
	if strings.Contains(path, "{id}") {
		otherStatus := 403
		if method == "get" {
			otherStatus = 404
		}
		cases = append(cases, openAPIA2Case{name: "other owner hidden or forbidden", id: "102", status: otherStatus}, openAPIA2Case{name: "unknown ID", id: "999", status: 404}, openAPIA2Case{name: "zero ID parses", id: "0", status: 404}, openAPIA2Case{name: "negative ID parses", id: "-1", status: 404}, openAPIA2Case{name: "invalid ID", id: "bad", status: 400, requestInvalid: true}, openAPIA2Case{name: "overflow ID", id: "9223372036854775808", status: 400, requestInvalid: true}, openAPIA2Case{name: "signed decimal ID", id: "+101", status: 200})
	}
	if method == "post" || method == "put" {
		for _, body := range []string{`[]`, `{"name":1}`, `{"quota":-1}`, `{"rate_limit_5h":-0.1}`, `{"rate_limit_1d":-1}`, `{"rate_limit_7d":-1}`, `{"quota":1e309}`, `{"group_id":1.5}`, `{"name":`, ``} {
			if method == "post" && strings.HasPrefix(body, `{"quota"`) {
				body = strings.Replace(body, "{", `{"name":"valid",`, 1)
			}
			cases = append(cases, openAPIA2Case{name: "bad JSON or numeric " + body, body: body, requestInvalid: true, status: 400})
		}
		for _, body := range []string{`{"ip_whitelist":["bad-IP"]}`, `{"ip_blacklist":["999.0.0.1"]}`} {
			if method == "post" {
				body = strings.Replace(body, "{", `{"name":"valid",`, 1)
			}
			cases = append(cases, openAPIA2Case{name: "invalid IP " + body, body: body, status: 400, reason: "INVALID_IP_PATTERN"})
		}
		for _, g := range []struct{ id, status int }{{11, 200}, {12, 403}, {13, 200}, {14, 200}, {15, 403}, {999, 404}} {
			cases = append(cases, openAPIA2Case{name: fmt.Sprintf("bind group %d", g.id), body: fmt.Sprintf(`{"name":"group test","group_id":%d}`, g.id), status: g.status})
		}
		cases = append(cases, openAPIA2Case{name: "restricted public group", body: `{"name":"group test","group_id":11}`, change: func(f *openAPIA2Fixture) { f.a1.users.user.RestrictPublicGroups = true }, status: 403}, openAPIA2Case{name: "unknown fields ignored", body: `{"name":"known","future":{"nested":[1,true,null]}}`, status: 200}, openAPIA2Case{name: "null optional fields", body: `{"name":"known","quota":null,"group_id":null,"ip_whitelist":null,"rate_limit_5h":null}`, status: 200})
	}
	if method == "post" {
		cases = append(cases,
			openAPIA2Case{name: "missing name", body: `{}`, requestInvalid: true, status: 400},
			openAPIA2Case{name: "empty name", body: `{"name":""}`, requestInvalid: true, status: 400},
			openAPIA2Case{name: "nonpositive expiry", body: `{"name":"x","expires_in_days":0}`, requestInvalid: true, status: 400},
			openAPIA2Case{name: "custom key too short", body: `{"name":"x","custom_key":"short"}`, status: 400, reason: "API_KEY_TOO_SHORT"},
			openAPIA2Case{name: "custom key invalid chars", body: `{"name":"x","custom_key":"long-enough-but-!"}`, status: 400, reason: "API_KEY_INVALID_CHARS"},
			openAPIA2Case{name: "duplicate custom key", body: `{"name":"x","custom_key":"` + openAPIA2OwnKey + `"}`, status: 409, reason: "API_KEY_EXISTS"},
			openAPIA2Case{name: "custom failure attempts limit", change: func(f *openAPIA2Fixture) { f.cache.attempts = 20 }, status: 429, reason: "API_KEY_RATE_LIMITED"},
			openAPIA2Case{name: "active key count limit", change: func(f *openAPIA2Fixture) { f.maxActive = 1 }, status: 403, reason: "API_KEY_COUNT_EXCEEDED"},
			openAPIA2Case{name: "creation hour limit", change: func(f *openAPIA2Fixture) { f.maxCreates = 1; f.cache.creates = 1 }, status: 429, reason: "API_KEY_CREATE_RATE_LIMITED"},
			openAPIA2Case{name: "create cache error fail open", change: func(f *openAPIA2Fixture) { f.maxCreates = 1; f.cache.fail = true }, status: 200},
			openAPIA2Case{name: "idempotency required in enforce mode", change: func(f *openAPIA2Fixture) { f.enforceIdem = true }, status: 400, reason: "IDEMPOTENCY_KEY_REQUIRED"},
			openAPIA2Case{name: "idempotency invalid header", headers: map[string]string{"Idempotency-Key": "bad key"}, status: 400, reason: "IDEMPOTENCY_KEY_INVALID"},
			openAPIA2Case{name: "idempotency store unavailable", headers: map[string]string{"Idempotency-Key": "synthetic-create"}, change: func(f *openAPIA2Fixture) { f.idem.fail = true }, status: 503, reason: "IDEMPOTENCY_STORE_UNAVAILABLE"},
			openAPIA2Case{name: "idempotency in progress", headers: map[string]string{"Idempotency-Key": "synthetic-create"}, change: func(f *openAPIA2Fixture) { f.idem.forcedStatus = service.IdempotencyStatusProcessing }, status: 409, reason: "IDEMPOTENCY_IN_PROGRESS"},
			openAPIA2Case{name: "populated creation", body: `{"name":"<key>","custom_key":"` + openAPIA2CreateKey + `","quota":0.125,"expires_in_days":2,"rate_limit_5h":1,"rate_limit_1d":2,"rate_limit_7d":3,"ip_whitelist":["192.0.2.0/24"],"ip_blacklist":["2001:db8::/32"]}`, status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, b map[string]any) {
				d := openAPIA1As[map[string]any](t, b["data"])
				if d["name"] != "&lt;key&gt;" || d["key"] != openAPIA2CreateKey || d["quota"] != json.Number("0.125") || f.keys.writes != 1 {
					t.Fatal("create transformations/credential contract changed")
				}
			}},
		)
	}
	if method == "put" {
		cases = append(cases, openAPIA2Case{name: "empty update preserves fields", body: `{}`, status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, _ map[string]any) {
			if !f.keys.fields.IsEmpty() {
				t.Fatal("empty update wrote fields")
			}
		}}, openAPIA2Case{name: "invalid status", body: `{"status":"disabled"}`, status: 400, requestInvalid: true}, openAPIA2Case{name: "invalid expiry date", body: `{"expires_at":"tomorrow"}`, status: 400}, openAPIA2Case{name: "clear expiry and reset counters", body: `{"expires_at":"","quota":0,"reset_quota":true,"reset_rate_limit_usage":true,"rate_limit_5h":0,"ip_whitelist":[]}`, status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, _ map[string]any) {
			k := f.keys.keys[101]
			if k.ExpiresAt != nil || k.QuotaUsed != 0 || k.Usage5h != 0 || len(k.IPWhitelist) != 0 || !f.keys.fields.QuotaUsed || !f.keys.fields.RateLimitUsage {
				t.Fatal("reset update changed")
			}
		}}, openAPIA2Case{name: "set expiry and inactive", body: `{"expires_at":"2035-01-02T03:04:05Z","status":"inactive"}`, status: 200}, openAPIA2Case{name: "null group cannot clear binding", body: `{"group_id":null}`, status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, _ map[string]any) {
			if f.keys.keys[101].GroupID == nil || f.keys.fields.GroupID {
				t.Fatal("null unexpectedly cleared group")
			}
		}})
	}
	if method == "get" && path == "/api/v1/keys" {
		cases = append(cases, openAPIA2Case{name: "query normalization", query: "?page=bad&page_size=1001&limit=5&group_id=bad&search=%20needle%20&sort_by=name&sort_order=asc&status=inactive", status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, _ map[string]any) {
			if f.keys.userID != 41 || f.keys.params.Page != 1 || f.keys.params.PageSize != 20 || f.keys.filters.GroupID != nil || f.keys.filters.Search != "needle" || f.keys.filters.Status != "inactive" || f.keys.params.SortBy != "name" || f.keys.params.SortOrder != "asc" {
				t.Fatal("query scoping/normalization drift")
			}
		}}, openAPIA2Case{name: "limit alias and signed group", query: "?page=2&limit=5&group_id=-1&search=" + strings.Repeat("a", 110), status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, _ map[string]any) {
			if f.keys.params.Page != 2 || f.keys.params.PageSize != 5 || f.keys.filters.GroupID == nil || *f.keys.filters.GroupID != -1 || len(f.keys.filters.Search) != 100 {
				t.Fatal("filter parsing drift")
			}
		}}, openAPIA2Case{name: "empty list remains array", change: func(f *openAPIA2Fixture) { delete(f.keys.keys, 101) }, status: 200})
	}
	if path == "/api/v1/groups/available" {
		cases = append(cases, openAPIA2Case{name: "visibility selection", status: 200, inspect: func(t *testing.T, _ *openAPIA2Fixture, b map[string]any) {
			groups := openAPIA1As[[]any](t, b["data"])
			ids := []string{}
			for _, g := range groups {
				ids = append(ids, fmt.Sprint(openAPIA1As[map[string]any](t, g)["id"]))
			}
			if strings.Join(ids, ",") != "11,13,14" {
				t.Fatalf("visible IDs %v", ids)
			}
		}}, openAPIA2Case{name: "subscriptions repository error", change: func(f *openAPIA2Fixture) { f.subs.fail = true }, status: 500}, openAPIA2Case{name: "empty available array", change: func(f *openAPIA2Fixture) { f.groups.groups = nil }, status: 200})
	}
	if path == "/api/v1/groups/rates" {
		cases = append(cases, openAPIA2Case{name: "nil rates encoded null", change: func(f *openAPIA2Fixture) { f.rates.rates = nil }, status: 200, inspect: func(t *testing.T, f *openAPIA2Fixture, b map[string]any) {
			if b["data"] != nil || f.rates.userID != 41 {
				t.Fatal("nullable rates/scoping drift")
			}
		}}, openAPIA2Case{name: "empty rates object", change: func(f *openAPIA2Fixture) { f.rates.rates = map[int64]float64{} }, status: 200})
	}
	for i := range cases {
		if cases[i].auth == "" {
			cases[i].auth = bearer
		}
		if cases[i].id == "" {
			cases[i].id = "101"
		}
		if cases[i].body == "" && !cases[i].requestInvalid {
			cases[i].body = validBody
		}
	}
	return cases
}
func openAPIA2VerifyResponses(t *testing.T, method, path string, op openAPIOperation) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range openAPIA2Cases(t, method, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA2NewFixture(t)
			if tc.change != nil {
				tc.change(f)
			}
			r := f.router(t)
			req := httptest.NewRequest(strings.ToUpper(method), openAPIA2URL(path, tc.id)+tc.query, strings.NewReader(tc.body))
			req.RemoteAddr = "203.0.113.9:1234"
			req.Header.Set("User-Agent", "openapi-fixture")
			req.Header.Set("Content-Type", "application/json")
			if tc.auth != "-" {
				req.Header.Set("Authorization", tc.auth)
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			err := openAPIValidateRequest(op, req, map[string]string{"id": tc.id}, []byte(tc.body))
			if tc.requestInvalid {
				if err == nil {
					t.Fatal("negative request passed contract")
				}
			} else if err != nil {
				t.Fatalf("request contract: %v", err)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatalf("response contract: %v body=%s", err, rec.Body.String())
			}
			var b map[string]any
			if err := openAPIDecode(rec.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			authCodes := map[string]string{"missing auth": "UNAUTHORIZED", "API key is not session JWT": "UNAUTHORIZED", "wrong scheme": "INVALID_AUTH_HEADER", "empty bearer": "EMPTY_TOKEN", "invalid signature": "INVALID_TOKEN", "expired": "TOKEN_EXPIRED", "revoked token version": "TOKEN_REVOKED", "inactive user": "USER_INACTIVE", "missing user": "USER_NOT_FOUND", "auth store unavailable": "INTERNAL_ERROR", "bound session mismatch revokes family": "SESSION_BINDING_MISMATCH", "real panel limiter": "RATE_LIMITED"}
			if want, ok := authCodes[tc.name]; ok && b["code"] != want {
				t.Fatalf("auth error code=%v want=%s", b["code"], want)
			}
			if tc.name == "real panel limiter" && rec.Header().Get("Retry-After") != "2" {
				t.Fatal("limiter retry header changed")
			}
			if tc.reason != "" && b["reason"] != tc.reason {
				t.Fatalf("reason=%v want=%s", b["reason"], tc.reason)
			}
			if strings.Contains(rec.Body.String(), openAPIA2OtherKey) {
				t.Fatal("other-owner secret leaked")
			}
			if rec.Code != 200 && f.keys.writes != 0 {
				t.Fatal("rejected request mutated key")
			}
			if rec.Code == 401 && f.keys.reads != 0 {
				t.Fatal("unauthenticated request reached key repository")
			}
			if rec.Code == 200 && strings.Contains(path, "/keys") && method != "delete" {
				if !strings.Contains(rec.Body.String(), `"key":`) && tc.name != "empty list remains array" {
					t.Fatal("owner credential missing")
				}
			}
			if tc.inspect != nil {
				tc.inspect(t, f, b)
			}
			seen[fmt.Sprint(tc.status)] = true
		})
	}
	for code := range op.Responses {
		if !seen[code] {
			t.Errorf("documented response lacks executable case: %s", code)
		}
	}
}
