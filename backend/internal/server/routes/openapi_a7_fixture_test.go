package routes_test

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"testing"
	"time"
)

type openAPIA7Repo struct {
	*openAPIA5V2
	updates, writes, expected int
	input                     service.ChannelMonitorV2Config
	nilResult                 bool
	conflict                  bool
}

func (r *openAPIA7Repo) UpdateConfig(_ context.Context, c service.ChannelMonitorV2Config, version int) (*service.ChannelMonitorV2Config, error) {
	r.updates++
	r.input = c
	r.expected = version
	if r.fail {
		return nil, fmt.Errorf("PRIVATE-A7-REPOSITORY")
	}
	if r.conflict {
		return nil, service.ErrChannelMonitorV2ConfigConflict
	}
	r.writes++
	c.Version = version + 1
	c.UpdatedAt = time.Date(2035, 2, 3, 4, 5, 6, 0, time.UTC)
	r.config = c
	return &c, nil
}

type openAPIA7Fixture struct {
	a1              *openAPIA1Fixture
	v2              *openAPIA7Repo
	repo            service.ChannelMonitorV2Repository
	noRuntimeReader bool
	audit           *openAPIA6Audit
	auditService    *service.AuditLogService
}

func openAPIA7NewFixture(t *testing.T) *openAPIA7Fixture {
	a := openAPIA1NewFixture(t)
	at := time.Now()
	a.users.user.LastActiveAt = &at
	a.settings.values[service.SettingKeyChannelMonitorEnabled] = "true"
	a.settings.values[service.SettingKeyChannelMonitorMode] = "v2"
	v := openAPIA5NewFixture(t, "/api/v1/channel-monitor-v2/snapshot").v2
	return &openAPIA7Fixture{a1: a, v2: &openAPIA7Repo{openAPIA5V2: v}, audit: &openAPIA6Audit{}}
}
func (f *openAPIA7Fixture) router(t *testing.T) *gin.Engine {
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
	repo := f.repo
	if repo == nil {
		repo = f.v2
	}
	svc := service.NewChannelMonitorV2Service(repo)
	if !f.noRuntimeReader {
		svc.SetRuntimeReader(settings)
	}
	h := openAPIHandlers(t)
	// Nil authorizer is intentional: every admin route must bypass AvailableGroups.
	h.ChannelMonitorV2 = handler.NewChannelMonitorV2Handler(svc, nil)
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

func (r *openAPIA7Repo) GetDimensions(ctx context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config) (*service.ChannelMonitorV2Dimensions, error) {
	if r.nilResult {
		return nil, nil
	}
	return r.openAPIA5V2.GetDimensions(ctx, f, c)
}

func (r *openAPIA7Repo) GetSnapshot(ctx context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2Snapshot, error) {
	if r.nilResult {
		return nil, nil
	}
	return r.openAPIA5V2.GetSnapshot(ctx, f, c, admin)
}

func (r *openAPIA7Repo) GetModels(ctx context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2ModelRow], error) {
	if r.nilResult {
		return nil, nil
	}
	return r.openAPIA5V2.GetModels(ctx, f, c, admin)
}

func (r *openAPIA7Repo) GetMatrix(ctx context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config, group service.ChannelMonitorV2GroupBy, admin bool) (*service.ChannelMonitorV2Matrix, error) {
	if r.nilResult {
		return nil, nil
	}
	return r.openAPIA5V2.GetMatrix(ctx, f, c, group, admin)
}

func (r *openAPIA7Repo) GetErrors(ctx context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2ErrorRow], error) {
	if r.nilResult {
		return nil, nil
	}
	return r.openAPIA5V2.GetErrors(ctx, f, c, admin)
}

func (r *openAPIA7Repo) GetUsers(ctx context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config, admin bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2UserRow], error) {
	if r.nilResult {
		return nil, nil
	}
	return r.openAPIA5V2.GetUsers(ctx, f, c, admin)
}
