package routes_test

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
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

type openAPIA9Repo struct {
	service.ChannelRepository
	rows           map[int64]service.Channel
	fail           string
	calls, writes  int
	conflict       bool
	params         pagination.PaginationParams
	status, search string
}

func (r *openAPIA9Repo) hit(op string) error {
	r.calls++
	if r.fail == op || r.fail == "all" {
		return fmt.Errorf("PRIVATE-A9-STORE")
	}
	return nil
}
func (r *openAPIA9Repo) GetByID(_ context.Context, id int64) (*service.Channel, error) {
	if e := r.hit("get"); e != nil {
		return nil, e
	}
	v, ok := r.rows[id]
	if !ok || r.fail == "get-missing" {
		return nil, service.ErrChannelNotFound
	}
	c := openAPIA8Clone(v)
	return &c, nil
}
func (r *openAPIA9Repo) Create(_ context.Context, c *service.Channel) error {
	if e := r.hit("create"); e != nil {
		return e
	}
	c.ID = 701
	c.CreatedAt = time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	c.UpdatedAt = c.CreatedAt
	r.rows[c.ID] = openAPIA8Clone(*c)
	r.writes++
	return nil
}
func (r *openAPIA9Repo) Update(_ context.Context, c *service.Channel) error {
	if e := r.hit("update"); e != nil {
		return e
	}
	c.UpdatedAt = time.Date(2035, 2, 2, 3, 4, 5, 0, time.UTC)
	r.rows[c.ID] = openAPIA8Clone(*c)
	r.writes++
	return nil
}
func (r *openAPIA9Repo) Delete(_ context.Context, id int64) error {
	if e := r.hit("delete"); e != nil {
		return e
	}
	if _, ok := r.rows[id]; !ok {
		return service.ErrChannelNotFound
	}
	delete(r.rows, id)
	r.writes++
	return nil
}
func (r *openAPIA9Repo) List(_ context.Context, p pagination.PaginationParams, status, search string) ([]service.Channel, *pagination.PaginationResult, error) {
	r.params = p
	r.status = status
	r.search = search
	if e := r.hit("list"); e != nil {
		return nil, nil, e
	}
	out := []service.Channel{}
	for _, v := range r.rows {
		if (status == "" || v.Status == status) && (search == "" || strings.Contains(strings.ToLower(v.Name+v.Description), strings.ToLower(search))) {
			out = append(out, openAPIA8Clone(v))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, &pagination.PaginationResult{Total: int64(len(out))}, nil
}
func (r *openAPIA9Repo) ListAll(context.Context) ([]service.Channel, error) {
	if e := r.hit("allrows"); e != nil {
		return nil, e
	}
	out := []service.Channel{}
	for _, v := range r.rows {
		out = append(out, openAPIA8Clone(v))
	}
	return out, nil
}
func (r *openAPIA9Repo) ExistsByName(ctx context.Context, name string) (bool, error) {
	return r.ExistsByNameExcluding(ctx, name, 0)
}
func (r *openAPIA9Repo) ExistsByNameExcluding(_ context.Context, name string, id int64) (bool, error) {
	if e := r.hit("exists"); e != nil {
		return false, e
	}
	for _, v := range r.rows {
		if v.ID != id && v.Name == name {
			return true, nil
		}
	}
	return false, nil
}
func (r *openAPIA9Repo) GetGroupIDs(_ context.Context, id int64) ([]int64, error) {
	if e := r.hit("groups"); e != nil {
		return nil, e
	}
	return append([]int64{}, r.rows[id].GroupIDs...), nil
}
func (r *openAPIA9Repo) GetGroupsInOtherChannels(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	if e := r.hit("conflicts"); e != nil {
		return nil, e
	}
	if r.conflict {
		return ids, nil
	}
	return nil, nil
}
func (r *openAPIA9Repo) GetGroupPlatforms(_ context.Context, ids []int64) (map[int64]string, error) {
	if e := r.hit("platforms"); e != nil {
		return nil, e
	}
	out := map[int64]string{}
	for _, id := range ids {
		out[id] = "anthropic"
	}
	return out, nil
}

type openAPIA9Fixture struct {
	a1             *openAPIA1Fixture
	repo           *openAPIA9Repo
	groups         *openAPIA2Groups
	subs           *openAPIA2Subscriptions
	audit          *openAPIA6Audit
	auditService   *service.AuditLogService
	repoOverride   service.ChannelRepository
	billing        *service.BillingService
	availableUsers *openAPIA9AvailabilityUsers
}

func openAPIA9NewFixture(t *testing.T) *openAPIA9Fixture {
	a := openAPIA1NewFixture(t)
	now := time.Now()
	a.users.user.LastActiveAt = &now
	a.settings.values[service.SettingKeyAvailableChannelsEnabled] = "true"
	price := 0.125
	mx := 100
	factor := 2.0
	p := service.ChannelModelPricing{ID: 81, Platform: "anthropic", Models: []string{"synthetic-a9"}, BillingMode: service.BillingModeToken, InputPrice: &price, FastMultiplier: &factor, ReasoningEffortMultipliers: map[string]float64{"high": 2}, Intervals: []service.PricingInterval{{ID: 82, MinTokens: 0, MaxTokens: &mx, InputPrice: &price, InputMultiplier: &factor, TierLabel: "Synthetic"}}, TimePricing: &service.ChannelTimePricing{Timezone: "UTC", Periods: []service.ChannelTimePricingPeriod{{StartTime: "08:00", EndTime: "09:00", Multiplier: 2}}}}
	c := service.Channel{ID: 301, Name: "Visible", Description: "Synthetic description", Status: service.StatusActive, GroupIDs: []int64{13}, ModelPricing: []service.ChannelModelPricing{p}, ModelMapping: map[string]map[string]string{"anthropic": {"alias-a9": "synthetic-a9"}}, Features: "[]", FeaturesConfig: map[string]any{"nested": []any{true, nil, 2.5, map[string]any{"x": "y"}}}, CreatedAt: now.UTC(), UpdatedAt: now.UTC()}
	return &openAPIA9Fixture{a1: a, repo: &openAPIA9Repo{rows: map[int64]service.Channel{301: c}}, groups: &openAPIA2Groups{groups: []service.Group{{ID: 13, Name: "Group", Platform: "anthropic", Status: service.StatusActive, SubscriptionType: "standard", RateMultiplier: 1, PeakRateMultiplier: 2, I18n: service.GroupI18n{"en": {Name: "Translated"}}}}}, subs: &openAPIA2Subscriptions{groups: map[int64]bool{}}, audit: &openAPIA6Audit{}}
}
func (f *openAPIA9Fixture) router(t *testing.T) *gin.Engine {
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
	repo := f.repoOverride
	if repo == nil {
		repo = f.repo
	}
	// Constructor only: no Initialize, ForceUpdate, remote client or scheduler.
	pricing := service.NewPricingService(cfg, nil)
	billing := f.billing
	if billing == nil {
		billing = service.NewBillingService(cfg, pricing)
	}
	svc := service.NewChannelService(repo, f.groups, nil, pricing, nil)
	keyUsers := service.UserRepository(f.a1.users)
	if f.availableUsers != nil {
		keyUsers = f.availableUsers
	}
	keys := service.NewAPIKeyService(nil, keyUsers, f.groups, f.subs, nil, nil, nil)
	h := openAPIHandlers(t)
	h.Admin.Channel = admin.NewChannelHandler(svc, billing, pricing)
	h.AvailableChannel = handler.NewAvailableChannelHandler(svc, keys, settings)
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
