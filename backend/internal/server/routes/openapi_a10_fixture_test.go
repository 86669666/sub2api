package routes_test

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

type openAPIA10Repo struct {
	rows          map[int64]*model.ErrorPassthroughRule
	fail          string
	calls, writes int
	ids           []int64
	delegate      service.ErrorPassthroughRepository
}

func (r *openAPIA10Repo) hit(op string) error {
	r.calls++
	if r.fail == op || r.fail == "all" {
		return fmt.Errorf("PRIVATE-A10-REPO")
	}
	return nil
}
func (r *openAPIA10Repo) List(ctx context.Context) ([]*model.ErrorPassthroughRule, error) {
	if e := r.hit("list"); e != nil {
		return nil, e
	}
	if r.delegate != nil {
		return r.delegate.List(ctx)
	}
	out := make([]*model.ErrorPassthroughRule, 0, len(r.rows))
	for _, v := range r.rows {
		out = append(out, openAPIA8Clone(v))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out, nil
}
func (r *openAPIA10Repo) GetByID(ctx context.Context, id int64) (*model.ErrorPassthroughRule, error) {
	r.ids = append(r.ids, id)
	if e := r.hit("get"); e != nil {
		return nil, e
	}
	if r.delegate != nil {
		return r.delegate.GetByID(ctx, id)
	}
	return openAPIA8Clone(r.rows[id]), nil
}
func (r *openAPIA10Repo) Create(ctx context.Context, v *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	if e := r.hit("create"); e != nil {
		return nil, e
	}
	if r.delegate != nil {
		return r.delegate.Create(ctx, v)
	}
	c := openAPIA8Clone(v)
	c.ID = 901
	c.CreatedAt = time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	c.UpdatedAt = c.CreatedAt
	r.rows[c.ID] = c
	r.writes++
	return openAPIA8Clone(c), nil
}
func (r *openAPIA10Repo) Update(ctx context.Context, v *model.ErrorPassthroughRule) (*model.ErrorPassthroughRule, error) {
	if e := r.hit("update"); e != nil {
		return nil, e
	}
	if r.delegate != nil {
		return r.delegate.Update(ctx, v)
	}
	if r.fail == "update-missing" {
		return nil, &dbent.NotFoundError{}
	}
	c := openAPIA8Clone(v)
	c.CreatedAt = r.rows[v.ID].CreatedAt
	c.UpdatedAt = c.CreatedAt.Add(time.Hour)
	r.rows[c.ID] = c
	r.writes++
	return openAPIA8Clone(c), nil
}
func (r *openAPIA10Repo) Delete(ctx context.Context, id int64) error {
	r.ids = append(r.ids, id)
	if e := r.hit("delete"); e != nil {
		return e
	}
	if r.delegate != nil {
		return r.delegate.Delete(ctx, id)
	}
	if r.rows[id] == nil {
		return &dbent.NotFoundError{}
	}
	delete(r.rows, id)
	r.writes++
	return nil
}

type openAPIA10Cache struct {
	fail                                       bool
	sets, invalidates, notifies, subscriptions int
	callback                                   func()
}

func (c *openAPIA10Cache) Get(context.Context) ([]*model.ErrorPassthroughRule, bool) {
	return nil, false
}
func (c *openAPIA10Cache) Set(context.Context, []*model.ErrorPassthroughRule) error {
	c.sets++
	if c.fail {
		return fmt.Errorf("PRIVATE-A10-CACHE")
	}
	return nil
}
func (c *openAPIA10Cache) Invalidate(context.Context) error {
	c.invalidates++
	if c.fail {
		return fmt.Errorf("PRIVATE-A10-CACHE")
	}
	return nil
}
func (c *openAPIA10Cache) NotifyUpdate(context.Context) error {
	c.notifies++
	if c.fail {
		return fmt.Errorf("PRIVATE-A10-CACHE")
	}
	return nil
}

// No goroutine, Redis connection, or automatic callback. Tests explicitly invoke it.
func (c *openAPIA10Cache) SubscribeUpdates(_ context.Context, fn func()) {
	c.subscriptions++
	c.callback = fn
}

type openAPIA10Fixture struct {
	a1           *openAPIA1Fixture
	repo         *openAPIA10Repo
	repoOverride service.ErrorPassthroughRepository
	cache        *openAPIA10Cache
	svc          *service.ErrorPassthroughService
	audit        *openAPIA6Audit
	auditService *service.AuditLogService
	startupReads int
}

func openAPIA10NewFixture(t *testing.T) *openAPIA10Fixture {
	a := openAPIA1NewFixture(t)
	now := time.Now()
	a.users.user.LastActiveAt = &now
	code := 429
	msg := "literal message"
	desc := "description"
	row := &model.ErrorPassthroughRule{ID: 301, Name: "Rule", Enabled: true, Priority: -2, ErrorCodes: []int{429, 503}, Keywords: []string{"quota", "Busy"}, MatchMode: "all", Platforms: []string{"OpenAI", "custom"}, PassthroughCode: true, ResponseCode: &code, PassthroughBody: true, CustomMessage: &msg, SkipMonitoring: true, Description: &desc, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	return &openAPIA10Fixture{a1: a, repo: &openAPIA10Repo{rows: map[int64]*model.ErrorPassthroughRule{301: row}}, audit: &openAPIA6Audit{}}
}
func (f *openAPIA10Fixture) router(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = openAPIA1SigningKey
	cfg.Gateway.MaxBodySize = 1024 * 1024
	cfg.Gateway.TextMaxBodySize = 1024 * 1024
	settings := service.NewSettingService(&openAPIA2Settings{openAPIA1Settings: f.a1.settings}, cfg)
	saved := map[string]string{}
	for k, v := range f.a1.settings.values {
		saved[k] = v
	}
	if e := settings.UpdateSettings(context.Background(), &service.SystemSettings{BackendModeEnabled: saved[service.SettingKeyBackendModeEnabled] == "true"}); e != nil {
		t.Fatal(e)
	}
	f.a1.settings.values = saved
	t.Cleanup(func() {
		if e := settings.UpdateSettings(context.Background(), &service.SystemSettings{}); e != nil {
			t.Error(e)
		}
	})
	users := service.NewUserService(f.a1.users, f.a1.settings, nil, nil)
	auth := service.NewAuthService(nil, f.a1.users, nil, f.a1.refresh, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	// Constructor always uses the owned memory repository, even in SQLmock tests.
	// Request-only faults/delegation are installed after the eager startup read.
	fail := f.repo.fail
	f.repo.fail = ""
	var cache service.ErrorPassthroughCache
	if f.cache != nil {
		cache = f.cache
	}
	f.svc = service.NewErrorPassthroughService(f.repo, cache)
	f.startupReads = f.repo.calls
	if f.startupReads != 1 {
		t.Fatalf("startup reads=%d", f.startupReads)
	}
	f.repo.calls = 0
	f.repo.writes = 0
	f.repo.ids = nil
	f.repo.fail = fail
	f.repo.delegate = f.repoOverride
	if f.cache != nil {
		f.cache.sets = 0
		f.cache.invalidates = 0
		f.cache.notifies = 0
	}
	h := openAPIHandlers(t)
	h.Admin.ErrorPassthrough = admin.NewErrorPassthroughHandler(f.svc)
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
