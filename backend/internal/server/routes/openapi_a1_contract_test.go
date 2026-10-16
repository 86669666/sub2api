package routes_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const openAPIA1Public = "/api/v1/settings/public"
const openAPIA1Version = "/api/v1/admin/system/version"

// Deliberately public test-only values, never loaded from config/env/keychain.
const openAPIA1SigningKey = "openapi-a1-synthetic-signing-fixture-never-deploy"
const openAPIA1AdminKey = "openapi-a1-synthetic-admin-fixture-never-deploy"

type openAPIA1Settings struct {
	service.SettingRepository
	values      map[string]string
	getErrKey   string
	multipleErr bool
}

func (s *openAPIA1Settings) GetValue(_ context.Context, key string) (string, error) {
	if key == s.getErrKey {
		return "", errors.New("synthetic repository failure")
	}
	if v, ok := s.values[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}
func (s *openAPIA1Settings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if s.multipleErr {
		return nil, errors.New("synthetic settings unavailable")
	}
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := s.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

type openAPIA1Users struct {
	service.UserRepository
	user      *service.User
	lookupErr error
	adminErr  bool
}

func (s *openAPIA1Users) GetByID(_ context.Context, id int64) (*service.User, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	if id != s.user.ID {
		return nil, service.ErrUserNotFound
	}
	u := *s.user
	return &u, nil
}
func (s *openAPIA1Users) GetFirstAdmin(context.Context) (*service.User, error) {
	if s.adminErr {
		return nil, errors.New("synthetic no admin")
	}
	u := *s.user
	return &u, nil
}
func (*openAPIA1Users) GetUserAvatar(context.Context, int64) (*service.UserAvatar, error) {
	return nil, nil
}

type openAPIA1Refresh struct {
	service.RefreshTokenCache
	revoked []string
}

func (c *openAPIA1Refresh) DeleteTokenFamily(_ context.Context, id string) error {
	c.revoked = append(c.revoked, id)
	return nil
}

type openAPIA1UpdateCache struct {
	value string
	reads int
}

func (c *openAPIA1UpdateCache) GetUpdateInfo(context.Context) (string, error) {
	c.reads++
	return c.value, nil
}
func (c *openAPIA1UpdateCache) SetUpdateInfo(_ context.Context, v string, _ time.Duration) error {
	c.value = v
	return nil
}

type openAPIA1Release struct {
	service.GitHubReleaseClient
	calls int
}

func (c *openAPIA1Release) FetchLatestRelease(context.Context, string) (*service.GitHubRelease, error) {
	c.calls++
	return nil, errors.New("synthetic upstream unavailable")
}

// Redis protocol hook is an in-memory counter result only, not a Redis server
// or Lua integration test. All dialing/pipelines/unexpected commands fail the
// fixture. Production PanelRateLimiter/RateLimiter still produce the response.
type openAPIA1Redis struct {
	t     *testing.T
	count int64
	calls int
	fail  bool
}

func (h *openAPIA1Redis) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		h.t.Error("fixture attempted network dial")
		return nil, errors.New("network forbidden")
	}
}
func (h *openAPIA1Redis) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error {
		h.t.Error("unexpected pipeline")
		return errors.New("unexpected pipeline")
	}
}
func (h *openAPIA1Redis) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		h.calls++
		if h.fail {
			return errors.New("synthetic counter unavailable")
		}
		switch cmd.Name() {
		case "evalsha", "eval":
			args := cmd.Args()
			if len(args) < 5 || !strings.HasPrefix(fmt.Sprint(args[3]), "rate_limit:panel:") {
				h.t.Fatalf("unexpected counter arguments: %v", args)
			}
			openAPIA1As[*redis.Cmd](h.t, cmd).SetVal([]any{h.count, int64(0)})
		case "pttl":
			openAPIA1As[*redis.DurationCmd](h.t, cmd).SetVal(1500 * time.Millisecond)
		default:
			h.t.Errorf("unexpected redis command %s", cmd.Name())
			return errors.New("unexpected command")
		}
		return nil
	}
}

type openAPIA1Fixture struct {
	settings *openAPIA1Settings
	users    *openAPIA1Users
	refresh  *openAPIA1Refresh
	cache    *openAPIA1UpdateCache
	release  *openAPIA1Release
	counter  *openAPIA1Redis
}

func openAPIA1NewFixture(t *testing.T) *openAPIA1Fixture {
	return &openAPIA1Fixture{
		settings: &openAPIA1Settings{values: map[string]string{
			service.SettingKeyAdminAPIKey:         openAPIA1AdminKey,
			"admin_compliance_acknowledgement:41": `{"version":"` + service.AdminComplianceVersion + `"}`,
		}},
		users:   &openAPIA1Users{user: &service.User{ID: 41, Email: "openapi@example.invalid", Role: service.RoleAdmin, Status: service.StatusActive, TokenVersion: 7, TokenVersionResolved: true}},
		refresh: &openAPIA1Refresh{}, cache: &openAPIA1UpdateCache{value: fmt.Sprintf(`{"latest":"a1-fixture-version","timestamp":%d}`, time.Now().Unix())}, release: &openAPIA1Release{}, counter: &openAPIA1Redis{t: t, count: 1},
	}
}
func (f *openAPIA1Fixture) router(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = openAPIA1SigningKey
	cfg.Gateway.MaxBodySize = 1024 * 1024
	cfg.Gateway.TextMaxBodySize = 1024 * 1024
	cfg.Pricing.DataDir = t.TempDir()
	settings := service.NewSettingService(f.settings, cfg)
	users := service.NewUserService(f.users, f.settings, nil, nil)
	auth := service.NewAuthService(nil, f.users, nil, f.refresh, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	h := openAPIHandlers(t)
	h.Setting = handler.NewSettingHandler(settings, "a1-fixture-version")
	h.Admin.System = admin.NewSystemHandler(service.NewUpdateService(f.cache, f.release, "a1-fixture-version", "source"), nil)
	client := redis.NewClient(&redis.Options{Addr: "openapi-fixture.invalid:0", MaxRetries: -1})
	client.AddHook(f.counter)
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	r := gin.New()
	// No observation-abort middleware: actual handlers and admin authentication run.
	// Non-target auth seams fail visibly if accidentally reached by either target.
	forbidden := func(c *gin.Context) { t.Error("unrelated auth seam reached"); c.AbortWithStatus(599) }
	server.SetupRouter(r, h, middleware.JWTAuthMiddleware(forbidden), middleware.OptionalJWTAuthMiddleware(forbidden), middleware.NewAdminAuthMiddleware(auth, users, settings, nil), middleware.APIKeyAuthMiddleware(forbidden), middleware.NewAuditLogMiddleware(nil), middleware.StepUpAuthMiddleware(forbidden), nil, nil, nil, settings, nil, cfg, client)
	t.Cleanup(func() { middleware.SetIngressRejectRecorder(nil) })
	return r
}
func openAPIA1Token(t *testing.T, mutate func(*service.JWTClaims), secret string) string {
	t.Helper()
	claims := &service.JWTClaims{UserID: 41, Role: service.RoleAdmin, TokenVersion: 7, SessionID: "synthetic-session", RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
	if mutate != nil {
		mutate(claims)
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

type openAPIA1Case struct {
	name    string
	headers map[string]string
	change  func(*openAPIA1Fixture)
	status  int
	code    string
	query   string
	inspect func(*testing.T, *openAPIA1Fixture, map[string]any, *httptest.ResponseRecorder)
}

func openAPIA1Cases(t *testing.T, path string) []openAPIA1Case {
	token := openAPIA1Token(t, nil, openAPIA1SigningKey)
	bearer := func(s string) map[string]string { return map[string]string{"Authorization": "Bearer " + s} }
	key := map[string]string{"x-api-key": openAPIA1AdminKey}
	limit := func(f *openAPIA1Fixture) {
		f.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"public_ip_rpm":1,"exempt_admin":false}`
		f.counter.count = 2
	}
	if path == openAPIA1Public {
		return []openAPIA1Case{
			{name: "defaults and anonymous", status: 200},
			{name: "invalid credentials ignored", headers: map[string]string{"Authorization": "Basic malformed", "x-api-key": "invalid"}, status: 200, query: "?ignored=value"},
			{name: "populated arrays and no secret leakage", status: 200, change: func(f *openAPIA1Fixture) {
				f.settings.values[service.SettingKeyRegistrationEmailSuffixWhitelist] = `["example.org"]`
				f.settings.values[service.SettingKeyCustomMenuItems] = `[{"id":"user","label":"Public","icon_svg":"svg","url":"https://example.invalid","visibility":"user","sort_order":2,"page_slug":"start","hide_open_button":true},{"id":"admin-only-canary","visibility":"admin"}]`
				f.settings.values[service.SettingKeyCustomEndpoints] = `[{"name":"Public","endpoint":"https://api.example.invalid","description":"copy"}]`
				f.settings.values[service.SettingKeyLoginAgreementDocuments] = `[{"id":"terms","title":"Terms","content_md":"Example terms"}]`
				f.settings.values[service.SettingKeyBalanceLowNotifyThreshold] = "0.125"
				for _, k := range []string{"github_oauth_client_secret", "wechat_connect_app_secret", "turnstile_secret_key", "smtp_password"} {
					f.settings.values[k] = "private-fixture-canary"
				}
			}, inspect: func(t *testing.T, _ *openAPIA1Fixture, body map[string]any, rec *httptest.ResponseRecorder) {
				if strings.Contains(rec.Body.String(), "private-fixture-canary") || strings.Contains(rec.Body.String(), "admin-only-canary") || strings.Contains(rec.Body.String(), openAPIA1AdminKey) {
					t.Fatal("private data leaked")
				}
				d := openAPIA1As[map[string]any](t, body["data"])
				if d["balance_low_notify_threshold"] != json.Number("0.125") {
					t.Fatal("decimal lost")
				}
				for _, k := range []string{"registration_email_suffix_whitelist", "login_agreement_documents", "custom_menu_items", "custom_endpoints"} {
					if len(openAPIA1As[[]any](t, d[k])) != 1 {
						t.Fatalf("fixture %s did not populate", k)
					}
				}
			}},
			{name: "omitted optional menu fields", status: 200, change: func(f *openAPIA1Fixture) { f.settings.values[service.SettingKeyCustomMenuItems] = `[{"id":"minimal"}]` }, inspect: func(t *testing.T, _ *openAPIA1Fixture, b map[string]any, _ *httptest.ResponseRecorder) {
				m := openAPIA1As[map[string]any](t, openAPIA1As[[]any](t, openAPIA1As[map[string]any](t, b["data"])["custom_menu_items"])[0])
				if _, ok := m["page_slug"]; ok {
					t.Fatal("empty page slug serialized")
				}
				if _, ok := m["hide_open_button"]; ok {
					t.Fatal("false hide button serialized")
				}
			}},
			{name: "stored null endpoint array", status: 200, change: func(f *openAPIA1Fixture) { f.settings.values[service.SettingKeyCustomEndpoints] = "null" }, inspect: func(t *testing.T, _ *openAPIA1Fixture, b map[string]any, _ *httptest.ResponseRecorder) {
				if openAPIA1As[map[string]any](t, b["data"])["custom_endpoints"] != nil {
					t.Fatal("expected null")
				}
			}},
			{name: "malformed stored arrays use empty defaults", status: 200, change: func(f *openAPIA1Fixture) {
				for _, k := range []string{service.SettingKeyCustomEndpoints, service.SettingKeyCustomMenuItems, service.SettingKeyRegistrationEmailSuffixWhitelist} {
					f.settings.values[k] = "not-json"
				}
			}},
			{name: "repository failure", status: 500, change: func(f *openAPIA1Fixture) { f.settings.multipleErr = true }},
			{name: "public rate limit", status: 429, code: "RATE_LIMITED", change: limit},
			{name: "public counter failure stays fail-open", status: 200, change: func(f *openAPIA1Fixture) { limit(f); f.counter.fail = true }},
		}
	}
	if path != openAPIA1Version {
		t.Fatalf("new operation needs an isolated executable response fixture: %s", path)
	}
	expired := openAPIA1Token(t, func(c *service.JWTClaims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour)) }, openAPIA1SigningKey)
	future := openAPIA1Token(t, func(c *service.JWTClaims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Hour)) }, openAPIA1SigningKey)
	bound := openAPIA1Token(t, func(c *service.JWTClaims) {
		c.BindingHash = (&service.SessionBinding{IP: "203.0.113.9", UserAgent: "openapi-fixture"}).Hash()
	}, openAPIA1SigningKey)
	upgrade := map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Protocol": "sub2api-admin, jwt." + token}
	return []openAPIA1Case{
		{name: "missing credentials", status: 401, code: "UNAUTHORIZED"},
		{name: "wrong auth scheme", headers: map[string]string{"Authorization": "Basic abc"}, status: 401, code: "UNAUTHORIZED"},
		{name: "missing bearer separator", headers: map[string]string{"Authorization": "Bearer"}, status: 401, code: "UNAUTHORIZED"},
		{name: "empty bearer", headers: bearer("  "), status: 401, code: "UNAUTHORIZED"},
		{name: "malformed JWT", headers: bearer("not-a-token"), status: 401, code: "INVALID_TOKEN"},
		{name: "invalid signature", headers: bearer(openAPIA1Token(t, nil, "different-synthetic-signing-key")), status: 401, code: "INVALID_TOKEN"},
		{name: "expired JWT", headers: bearer(expired), status: 401, code: "TOKEN_EXPIRED"},
		{name: "not yet valid JWT", headers: bearer(future), status: 401, code: "INVALID_TOKEN"},
		{name: "oversized token", headers: bearer(strings.Repeat("x", 20000)), status: 401, code: "INVALID_TOKEN"},
		{name: "valid admin JWT", headers: bearer(token), status: 200},
		{name: "case insensitive trimmed bearer", headers: map[string]string{"Authorization": "bEaReR  " + token + " "}, status: 200},
		{name: "valid admin key", headers: key, status: 200},
		{name: "invalid admin key", headers: map[string]string{"x-api-key": "invalid"}, status: 401, code: "INVALID_ADMIN_KEY"},
		{name: "unconfigured admin key", headers: key, status: 401, code: "INVALID_ADMIN_KEY", change: func(f *openAPIA1Fixture) { delete(f.settings.values, service.SettingKeyAdminAPIKey) }},
		{name: "admin key store failure", headers: key, status: 500, code: "INTERNAL_ERROR", change: func(f *openAPIA1Fixture) { f.settings.getErrKey = service.SettingKeyAdminAPIKey }},
		{name: "first admin missing", headers: key, status: 500, code: "INTERNAL_ERROR", change: func(f *openAPIA1Fixture) { f.users.adminErr = true }},
		{name: "nonadmin despite admin claim", headers: bearer(token), status: 403, code: "FORBIDDEN", change: func(f *openAPIA1Fixture) { f.users.user.Role = service.RoleUser }},
		{name: "inactive user", headers: bearer(token), status: 401, code: "USER_INACTIVE", change: func(f *openAPIA1Fixture) { f.users.user.Status = "disabled" }},
		{name: "revoked token version", headers: bearer(token), status: 401, code: "TOKEN_REVOKED", change: func(f *openAPIA1Fixture) { f.users.user.TokenVersion = 8 }},
		{name: "user not found", headers: bearer(token), status: 401, code: "USER_NOT_FOUND", change: func(f *openAPIA1Fixture) { f.users.lookupErr = service.ErrUserNotFound }},
		{name: "user store failure", headers: bearer(token), status: 500, code: "INTERNAL_ERROR", change: func(f *openAPIA1Fixture) { f.users.lookupErr = errors.New("synthetic user read failed") }},
		{name: "key precedence rejects invalid key with valid JWT", headers: map[string]string{"x-api-key": "bad", "Authorization": "Bearer " + token}, status: 401, code: "INVALID_ADMIN_KEY"},
		{name: "key precedence accepts valid key with invalid JWT", headers: map[string]string{"x-api-key": openAPIA1AdminKey, "Authorization": "Bearer bad"}, status: 200},
		{name: "upgrade JWT carrier still returns JSON", headers: upgrade, status: 200},
		{name: "upgrade JWT precedence over invalid key", headers: map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Protocol": "jwt." + token, "x-api-key": "bad"}, status: 200},
		{name: "invalid upgrade JWT never falls back to valid key", headers: map[string]string{"Connection": "Upgrade", "Upgrade": "websocket", "Sec-WebSocket-Protocol": "jwt.bad", "x-api-key": openAPIA1AdminKey}, status: 401, code: "INVALID_TOKEN"},
		{name: "subprotocol alone is not authentication", headers: map[string]string{"Sec-WebSocket-Protocol": "jwt." + token}, status: 401, code: "UNAUTHORIZED"},
		{name: "compliance acknowledgement required", headers: bearer(token), status: 423, code: "ADMIN_COMPLIANCE_ACK_REQUIRED", change: func(f *openAPIA1Fixture) { delete(f.settings.values, "admin_compliance_acknowledgement:41") }},
		{name: "compliance store failure", headers: bearer(token), status: 500, code: "INTERNAL_ERROR", change: func(f *openAPIA1Fixture) { f.settings.getErrKey = "admin_compliance_acknowledgement:41" }},
		{name: "admin configured rate limit", headers: key, status: 429, code: "RATE_LIMITED", change: limit},
		{name: "admin counter failure stays fail-open", headers: key, status: 200, change: func(f *openAPIA1Fixture) { limit(f); f.counter.fail = true }},
		{name: "force query ignored", headers: bearer(token), status: 200, query: "?force=true", inspect: func(t *testing.T, f *openAPIA1Fixture, _ map[string]any, _ *httptest.ResponseRecorder) {
			if f.release.calls != 0 {
				t.Fatal("force query used")
			}
		}},
		{name: "update source failure returns current version", headers: bearer(token), status: 200, change: func(f *openAPIA1Fixture) { f.cache.value = "" }, inspect: func(t *testing.T, f *openAPIA1Fixture, _ map[string]any, _ *httptest.ResponseRecorder) {
			if f.release.calls != 1 {
				t.Fatal("fallback not exercised")
			}
		}},
		{name: "bound session matches", headers: bearer(bound), status: 200, change: func(f *openAPIA1Fixture) { f.settings.values[service.SettingKeySessionBindingEnabled] = "true" }},
		{name: "bound session mismatch", headers: map[string]string{"Authorization": "Bearer " + bound, "User-Agent": "changed-fixture"}, status: 401, code: "SESSION_BINDING_MISMATCH", change: func(f *openAPIA1Fixture) { f.settings.values[service.SettingKeySessionBindingEnabled] = "true" }, inspect: func(t *testing.T, f *openAPIA1Fixture, _ map[string]any, _ *httptest.ResponseRecorder) {
			if len(f.refresh.revoked) != 1 || f.refresh.revoked[0] != "synthetic-session" {
				t.Fatal("session not revoked in memory cache")
			}
		}},
		{name: "legacy unbound session", headers: bearer(token), status: 200, change: func(f *openAPIA1Fixture) { f.settings.values[service.SettingKeySessionBindingEnabled] = "true" }},
	}
}
func openAPIA1VerifyResponses(t *testing.T, path string, op openAPIOperation) {
	t.Helper()
	seen := map[string]bool{}
	for _, tc := range openAPIA1Cases(t, path) {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA1NewFixture(t)
			if tc.change != nil {
				tc.change(f)
			}
			r := f.router(t)
			req := httptest.NewRequest("GET", path+tc.query, strings.NewReader("ignored-body"))
			req.RemoteAddr = "203.0.113.9:1234"
			req.Header.Set("User-Agent", "openapi-fixture")
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.status, rec.Body.String())
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatalf("contract: %v; body=%s", err, rec.Body.String())
			}
			var body map[string]any
			if err := openAPIDecode(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.code != "" && body["code"] != tc.code {
				t.Fatalf("code=%v want=%s", body["code"], tc.code)
			}
			if path == openAPIA1Version {
				if tc.status == 200 {
					if f.cache.reads == 0 || openAPIA1As[map[string]any](t, body["data"])["version"] != "a1-fixture-version" {
						t.Fatal("real version handler did not run")
					}
				} else if f.cache.reads != 0 {
					t.Fatal("rejected request reached version handler")
				}
			}
			if tc.status == 429 && (rec.Header().Get("Retry-After") != "2" || f.counter.calls != 2) {
				t.Fatal("real limiter did not round retry seconds")
			}
			if tc.inspect != nil {
				tc.inspect(t, f, body, rec)
			}
			seen[fmt.Sprint(tc.status)] = true
		})
	}
	for status := range op.Responses {
		if !seen[status] {
			t.Errorf("documented response %s lacks executable fixture", status)
		}
	}
}

// Checked assertions also keep fixture failures explicit under errcheck.
func openAPIA1As[T any](t *testing.T, value any) T {
	t.Helper()
	result, ok := value.(T)
	if !ok {
		t.Fatalf("expected %T, got %T", result, value)
	}
	return result
}
