package routes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"sort"
	"strings"
	"testing"
	"time"
)

const openAPIA8Secret = "A8SX-SYNTHETIC-KEY-CANARY"

func openAPIA8Clone[T any](v T) T {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	var c T
	if e = json.Unmarshal(b, &c); e != nil {
		panic(e)
	}
	return c
}

type openAPIA8Encryptor struct {
	encryptFail, decryptFail bool
	encrypts, decrypts       int
}

func (e *openAPIA8Encryptor) Encrypt(s string) (string, error) {
	e.encrypts++
	if e.encryptFail {
		return "", fmt.Errorf("synthetic encrypt failure")
	}
	return "cipher:" + strings.TrimSpace(s), nil
}
func (e *openAPIA8Encryptor) Decrypt(s string) (string, error) {
	e.decrypts++
	if e.decryptFail || !strings.HasPrefix(s, "cipher:") {
		return "", fmt.Errorf("synthetic decrypt failure")
	}
	return strings.TrimPrefix(s, "cipher:"), nil
}

type openAPIA8Monitors struct {
	*openAPIA5Legacy
	rows             map[int64]service.ChannelMonitor
	failOp           string
	writes, attempts int
	params           service.ChannelMonitorListParams
	historyModel     string
	historyLimit     int
	persisted        []*service.ChannelMonitorHistoryRow
	checked          int
}

func (r *openAPIA8Monitors) hit(op string) error {
	r.calls++
	if r.failOp == op || r.failOp == "all" {
		return fmt.Errorf("PRIVATE-A8-REPO")
	}
	return nil
}
func (r *openAPIA8Monitors) GetByID(_ context.Context, id int64) (*service.ChannelMonitor, error) {
	if e := r.hit("get"); e != nil {
		return nil, e
	}
	v, ok := r.rows[id]
	if !ok {
		return nil, service.ErrChannelMonitorNotFound
	}
	c := openAPIA8Clone(v)
	return &c, nil
}
func (r *openAPIA8Monitors) Create(_ context.Context, m *service.ChannelMonitor) error {
	r.attempts++
	if e := r.hit("create"); e != nil {
		return e
	}
	m.ID = 901 + int64(r.writes)
	m.CreatedAt = time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	m.UpdatedAt = m.CreatedAt
	r.rows[m.ID] = openAPIA8Clone(*m)
	r.writes++
	return nil
}
func (r *openAPIA8Monitors) Update(_ context.Context, m *service.ChannelMonitor) error {
	r.attempts++
	if e := r.hit("update"); e != nil {
		return e
	}
	m.UpdatedAt = time.Date(2035, 2, 2, 3, 4, 5, 0, time.UTC)
	r.rows[m.ID] = openAPIA8Clone(*m)
	r.writes++
	return nil
}
func (r *openAPIA8Monitors) Delete(_ context.Context, id int64) error {
	r.attempts++
	if e := r.hit("delete"); e != nil {
		return e
	}
	if _, ok := r.rows[id]; !ok {
		return service.ErrChannelMonitorNotFound
	}
	delete(r.rows, id)
	r.writes++
	return nil
}
func (r *openAPIA8Monitors) List(_ context.Context, p service.ChannelMonitorListParams) ([]*service.ChannelMonitor, int64, error) {
	r.params = p
	if e := r.hit("list"); e != nil {
		return nil, 0, e
	}
	out := []*service.ChannelMonitor{}
	if r.empty {
		return out, 0, nil
	}
	for _, v := range r.rows {
		if p.Provider != "" && p.Provider != v.Provider {
			continue
		}
		if p.Enabled != nil && *p.Enabled != v.Enabled {
			continue
		}
		if p.Search != "" && !strings.Contains(v.Name, p.Search) {
			continue
		}
		c := openAPIA8Clone(v)
		out = append(out, &c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, int64(len(out)), nil
}
func (r *openAPIA8Monitors) FindByDuplicateOperationID(_ context.Context, id string) (*service.ChannelMonitor, error) {
	if e := r.hit("find"); e != nil {
		return nil, e
	}
	for _, v := range r.rows {
		if v.DuplicateOperationID == id {
			c := openAPIA8Clone(v)
			return &c, nil
		}
	}
	return nil, nil
}
func (r *openAPIA8Monitors) ListHistory(_ context.Context, _ int64, model string, limit int) ([]*service.ChannelMonitorHistoryEntry, error) {
	r.historyModel = model
	r.historyLimit = limit
	if e := r.hit("history"); e != nil {
		return nil, e
	}
	if r.empty {
		return nil, nil
	}
	return r.history, nil
}
func (r *openAPIA8Monitors) InsertHistoryBatch(_ context.Context, rows []*service.ChannelMonitorHistoryRow) error {
	if e := r.hit("history-write"); e != nil {
		return e
	}
	r.persisted = openAPIA8Clone(rows)
	r.writes++
	return nil
}
func (r *openAPIA8Monitors) MarkChecked(_ context.Context, _ int64, _ time.Time) error {
	if e := r.hit("mark"); e != nil {
		return e
	}
	r.checked++
	return nil
}

type openAPIA8Templates struct {
	service.ChannelMonitorRequestTemplateRepository
	rows                    map[int64]service.ChannelMonitorRequestTemplate
	monitors                *openAPIA8Monitors
	calls, writes, attempts int
	fail                    string
	empty                   bool
	params                  service.ChannelMonitorRequestTemplateListParams
	ids                     []int64
}

func (r *openAPIA8Templates) hit(op string) error {
	r.calls++
	if r.fail == op || r.fail == "all" {
		return fmt.Errorf("PRIVATE-A8-TEMPLATE")
	}
	return nil
}
func (r *openAPIA8Templates) GetByID(_ context.Context, id int64) (*service.ChannelMonitorRequestTemplate, error) {
	if e := r.hit("get"); e != nil {
		return nil, e
	}
	v, ok := r.rows[id]
	if !ok {
		return nil, service.ErrChannelMonitorTemplateNotFound
	}
	c := openAPIA8Clone(v)
	return &c, nil
}
func (r *openAPIA8Templates) List(_ context.Context, p service.ChannelMonitorRequestTemplateListParams) ([]*service.ChannelMonitorRequestTemplate, error) {
	r.params = p
	if e := r.hit("list"); e != nil {
		return nil, e
	}
	out := []*service.ChannelMonitorRequestTemplate{}
	if r.empty {
		return out, nil
	}
	for _, v := range r.rows {
		if p.Provider != "" && p.Provider != v.Provider || p.APIMode != "" && p.APIMode != v.APIMode {
			continue
		}
		c := openAPIA8Clone(v)
		out = append(out, &c)
	}
	return out, nil
}
func (r *openAPIA8Templates) Create(_ context.Context, v *service.ChannelMonitorRequestTemplate) error {
	r.attempts++
	if e := r.hit("create"); e != nil {
		return e
	}
	v.ID = 701
	v.CreatedAt = time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	v.UpdatedAt = v.CreatedAt
	r.rows[v.ID] = openAPIA8Clone(*v)
	r.writes++
	return nil
}
func (r *openAPIA8Templates) Update(_ context.Context, v *service.ChannelMonitorRequestTemplate) error {
	r.attempts++
	if e := r.hit("update"); e != nil {
		return e
	}
	v.UpdatedAt = time.Date(2035, 2, 2, 3, 4, 5, 0, time.UTC)
	r.rows[v.ID] = openAPIA8Clone(*v)
	r.writes++
	return nil
}
func (r *openAPIA8Templates) Delete(_ context.Context, id int64) error {
	r.attempts++
	if e := r.hit("delete"); e != nil {
		return e
	}
	if _, ok := r.rows[id]; !ok {
		return service.ErrChannelMonitorTemplateNotFound
	}
	delete(r.rows, id)
	r.writes++
	return nil
}
func (r *openAPIA8Templates) CountAssociatedMonitors(_ context.Context, id int64) (int64, error) {
	if e := r.hit("count"); e != nil {
		return 0, e
	}
	var n int64
	for _, m := range r.monitors.rows {
		if m.TemplateID != nil && *m.TemplateID == id {
			n++
		}
	}
	return n, nil
}
func (r *openAPIA8Templates) ListAssociatedMonitors(_ context.Context, id int64) ([]*service.AssociatedMonitorBrief, error) {
	if e := r.hit("associated"); e != nil {
		return nil, e
	}
	out := []*service.AssociatedMonitorBrief{}
	if r.empty {
		return out, nil
	}
	for _, m := range r.monitors.rows {
		if m.TemplateID != nil && *m.TemplateID == id {
			out = append(out, &service.AssociatedMonitorBrief{ID: m.ID, Name: m.Name, Provider: m.Provider, APIMode: m.APIMode, Enabled: m.Enabled})
		}
	}
	return out, nil
}
func (r *openAPIA8Templates) ApplyToMonitors(_ context.Context, id int64, ids []int64) (int64, error) {
	r.ids = append([]int64{}, ids...)
	r.attempts++
	if e := r.hit("apply"); e != nil {
		return 0, e
	}
	tpl := r.rows[id]
	selected := map[int64]bool{}
	for _, i := range ids {
		selected[i] = true
	}
	var n int64
	for i, m := range r.monitors.rows {
		if selected[i] && m.TemplateID != nil && *m.TemplateID == id && m.Provider == tpl.Provider && m.APIMode == tpl.APIMode {
			m.ExtraHeaders = openAPIA8Clone(tpl.ExtraHeaders)
			m.BodyOverride = openAPIA8Clone(tpl.BodyOverride)
			m.BodyOverrideMode = tpl.BodyOverrideMode
			r.monitors.rows[i] = m
			n++
			r.monitors.writes++
		}
	}
	return n, nil
}

type openAPIA8Accounts struct {
	service.AccountRepository
	account *service.Account
	fail    bool
}

func (r *openAPIA8Accounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if r.fail || id != 77 {
		return nil, fmt.Errorf("synthetic missing account")
	}
	return r.account, nil
}

type openAPIA8Fixture struct {
	a1                                                       *openAPIA1Fixture
	monitors                                                 *openAPIA8Monitors
	templates                                                *openAPIA8Templates
	encryptor                                                *openAPIA8Encryptor
	accounts                                                 *openAPIA8Accounts
	useAccounts, noRuntimeReader, noCoordinator, enforceIdem bool
	idem                                                     *openAPIA2Idempotency
	audit                                                    *openAPIA6Audit
	auditService                                             *service.AuditLogService
	monitorRepo                                              service.ChannelMonitorRepository
	templateRepo                                             service.ChannelMonitorRequestTemplateRepository
}

func openAPIA8NewFixture(t *testing.T) *openAPIA8Fixture {
	a := openAPIA1NewFixture(t)
	at := time.Now()
	a.users.user.LastActiveAt = &at
	a.settings.values[service.SettingKeyChannelMonitorEnabled] = "true"
	a.settings.values[service.SettingKeyChannelMonitorMode] = "v1"
	old := openAPIA5NewFixture(t, "/api/v1/channel-monitors").legacy
	m := old.monitor
	tid := int64(501)
	m.TemplateID = &tid
	m.APIKey = "cipher:" + openAPIA8Secret
	m.Endpoint = "https://8.8.8.8/base"
	m.APIMode = "chat_completions"
	m.BodyOverrideMode = "off"
	m.CheckMode = "probe"
	m.ExtraHeaders = map[string]string{"X-Test": "synthetic"}
	m.CreatedAt = time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	m.UpdatedAt = m.CreatedAt
	m.IntervalSeconds = 60
	m.CreatedBy = 99
	m.BodyOverride = map[string]any{"nested": []any{true, nil, 2.5, map[string]any{"x": "y"}}}
	mr := &openAPIA8Monitors{openAPIA5Legacy: old, rows: map[int64]service.ChannelMonitor{301: m}}
	tpl := service.ChannelMonitorRequestTemplate{ID: 501, Name: "Template", Provider: "openai", APIMode: "chat_completions", Description: "Synthetic", ExtraHeaders: map[string]string{"X-Template": "yes"}, BodyOverrideMode: "off", BodyOverride: m.BodyOverride, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	return &openAPIA8Fixture{a1: a, monitors: mr, templates: &openAPIA8Templates{rows: map[int64]service.ChannelMonitorRequestTemplate{501: tpl}, monitors: mr}, encryptor: &openAPIA8Encryptor{}, accounts: &openAPIA8Accounts{account: &service.Account{ID: 77, Platform: "openai", Type: "oauth"}}, idem: &openAPIA2Idempotency{}, audit: &openAPIA6Audit{}}
}
func (f *openAPIA8Fixture) router(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = openAPIA1SigningKey
	cfg.Gateway.MaxBodySize = 1024 * 1024
	cfg.Gateway.TextMaxBodySize = 1024 * 1024
	cfg.Pricing.DataDir = t.TempDir()
	settings := service.NewSettingService(&openAPIA2Settings{openAPIA1Settings: f.a1.settings}, cfg)
	saved := map[string]string{}
	for k, v := range f.a1.settings.values {
		saved[k] = v
	}
	mode := saved[service.SettingKeyBackendModeEnabled]
	if err := settings.UpdateSettings(context.Background(), &service.SystemSettings{BackendModeEnabled: mode == "true"}); err != nil {
		t.Fatal(err)
	}
	f.a1.settings.values = saved
	t.Cleanup(func() {
		if err := settings.UpdateSettings(context.Background(), &service.SystemSettings{}); err != nil {
			t.Error(err)
		}
	})
	users := service.NewUserService(f.a1.users, f.a1.settings, nil, nil)
	auth := service.NewAuthService(nil, f.a1.users, nil, f.a1.refresh, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	mr := f.monitorRepo
	if mr == nil {
		mr = f.monitors
	}
	tr := f.templateRepo
	if tr == nil {
		tr = f.templates
	}
	svc := service.NewChannelMonitorService(mr, f.encryptor)
	if !f.noRuntimeReader {
		svc.SetRuntimeReader(settings)
	}
	if f.useAccounts {
		svc.SetQuotaFetcher(service.NewChannelMonitorQuotaFetcher(nil, nil, nil, f.accounts, cfg))
	}
	h := openAPIHandlers(t)
	h.Admin.ChannelMonitor = admin.NewChannelMonitorHandler(svc)
	h.Admin.ChannelMonitorTemplate = admin.NewChannelMonitorRequestTemplateHandler(service.NewChannelMonitorRequestTemplateService(tr))
	previous := service.DefaultIdempotencyCoordinator()
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	if f.noCoordinator {
		service.SetDefaultIdempotencyCoordinator(nil)
	} else {
		icfg := service.DefaultIdempotencyConfig()
		icfg.ObserveOnly = !f.enforceIdem
		service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(f.idem, icfg))
	}
	f.auditService = service.NewAuditLogService(f.audit, settings)
	f.auditService.Start()
	t.Cleanup(f.auditService.Stop)
	client := redis.NewClient(&redis.Options{Addr: "openapi-fixture.invalid:0", MaxRetries: -1})
	client.AddHook(f.a1.counter)
	t.Cleanup(func() {
		if e := client.Close(); e != nil {
			t.Error(e)
		}
	})
	forbidden := func(c *gin.Context) { t.Error("unrelated auth reached"); c.AbortWithStatus(599) }
	r := gin.New()
	server.SetupRouter(r, h, middleware.NewJWTAuthMiddleware(auth, users, settings, f.auditService), middleware.OptionalJWTAuthMiddleware(forbidden), middleware.NewAdminAuthMiddleware(auth, users, settings, f.auditService), middleware.APIKeyAuthMiddleware(forbidden), middleware.NewAuditLogMiddleware(f.auditService), middleware.StepUpAuthMiddleware(forbidden), nil, nil, nil, settings, nil, cfg, client)
	t.Cleanup(func() { middleware.SetIngressRejectRecorder(nil) })
	return r
}
