package routes_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const openAPIA2OwnKey = "sk-synthetic-a2-owner-fixture-never-deploy"
const openAPIA2OtherKey = "sk-synthetic-a2-other-owner-canary"
const openAPIA2CreateKey = "sk-synthetic-a2-create-fixture-never-deploy"

type openAPIA2Keys struct {
	service.APIKeyRepository
	keys          map[int64]service.APIKey
	fail          bool
	reads, writes int
	userID        int64
	params        pagination.PaginationParams
	filters       service.APIKeyListFilters
	fields        service.APIKeyUpdateFields
}

func (r *openAPIA2Keys) GetByID(_ context.Context, id int64) (*service.APIKey, error) {
	r.reads++
	if r.fail {
		return nil, errors.New("synthetic key store failure")
	}
	k, ok := r.keys[id]
	if !ok {
		return nil, service.ErrAPIKeyNotFound
	}
	return &k, nil
}
func (r *openAPIA2Keys) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	k, err := r.GetByID(ctx, id)
	if err != nil {
		return "", 0, err
	}
	return k.Key, k.UserID, nil
}
func (r *openAPIA2Keys) Create(_ context.Context, k *service.APIKey) error {
	if r.fail {
		return errors.New("synthetic create failure")
	}
	k.ID = 103
	k.CreatedAt = time.Now().UTC()
	k.UpdatedAt = k.CreatedAt
	r.keys[k.ID] = *k
	r.writes++
	return nil
}
func (r *openAPIA2Keys) Update(_ context.Context, k *service.APIKey, fields service.APIKeyUpdateFields) error {
	if r.fail {
		return errors.New("synthetic update failure")
	}
	r.keys[k.ID] = *k
	r.fields = fields
	r.writes++
	return nil
}
func (r *openAPIA2Keys) DeleteWithAudit(_ context.Context, id int64) error {
	if r.fail {
		return errors.New("synthetic delete failure")
	}
	delete(r.keys, id)
	r.writes++
	return nil
}
func (r *openAPIA2Keys) ExistsByKey(_ context.Context, key string) (bool, error) {
	if r.fail {
		return false, errors.New("synthetic exists failure")
	}
	for _, k := range r.keys {
		if k.Key == key {
			return true, nil
		}
	}
	return false, nil
}
func (r *openAPIA2Keys) CountByUserID(_ context.Context, id int64) (int64, error) {
	var n int64
	for _, k := range r.keys {
		if k.UserID == id {
			n++
		}
	}
	return n, nil
}
func (r *openAPIA2Keys) ListByUserID(_ context.Context, id int64, p pagination.PaginationParams, f service.APIKeyListFilters) ([]service.APIKey, *pagination.PaginationResult, error) {
	r.reads++
	r.userID = id
	r.params = p
	r.filters = f
	if r.fail {
		return nil, nil, errors.New("synthetic list failure")
	}
	out := []service.APIKey{}
	for _, k := range r.keys {
		if k.UserID == id {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	// This seam proves production scoping/filter arguments, not SQL filtering.
	return out, &pagination.PaginationResult{Total: int64(len(out)), Page: p.Page, PageSize: p.PageSize}, nil
}
func (r *openAPIA2Keys) ListAllByUserID(ctx context.Context, id int64, f service.APIKeyListFilters) ([]service.APIKey, error) {
	ks, _, err := r.ListByUserID(ctx, id, pagination.PaginationParams{Page: 1, PageSize: 1000}, f)
	return ks, err
}

type openAPIA2Groups struct {
	service.GroupRepository
	groups []service.Group
	fail   bool
}

func (r *openAPIA2Groups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if r.fail {
		return nil, errors.New("synthetic groups failure")
	}
	for _, g := range r.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, service.ErrGroupNotFound
}
func (r *openAPIA2Groups) ListActive(context.Context) ([]service.Group, error) {
	if r.fail {
		return nil, errors.New("synthetic groups failure")
	}
	return r.groups, nil
}

type openAPIA2Subscriptions struct {
	service.UserSubscriptionRepository
	groups map[int64]bool
	fail   bool
}

func (r *openAPIA2Subscriptions) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	if r.fail {
		return nil, errors.New("synthetic subscriptions failure")
	}
	out := []service.UserSubscription{}
	for id := range r.groups {
		out = append(out, service.UserSubscription{GroupID: id})
	}
	return out, nil
}
func (r *openAPIA2Subscriptions) GetActiveByUserIDAndGroupID(_ context.Context, _ int64, id int64) (*service.UserSubscription, error) {
	if r.groups[id] && !r.fail {
		return &service.UserSubscription{GroupID: id}, nil
	}
	return nil, errors.New("synthetic no active subscription")
}

type openAPIA2Rates struct {
	service.UserGroupRateRepository
	rates  map[int64]float64
	fail   bool
	userID int64
}

func (r *openAPIA2Rates) GetByUserID(_ context.Context, id int64) (map[int64]float64, error) {
	r.userID = id
	if r.fail {
		return nil, errors.New("synthetic rates failure")
	}
	return r.rates, nil
}

type openAPIA2Cache struct {
	service.APIKeyCache
	attempts      int
	creates       int64
	fail          bool
	invalidations int
}

func (r *openAPIA2Cache) GetCreateAttemptCount(context.Context, int64) (int, error) {
	if r.fail {
		return 0, errors.New("synthetic cache failure")
	}
	return r.attempts, nil
}
func (r *openAPIA2Cache) IncrementCreateAttemptCount(context.Context, int64) error {
	r.attempts++
	return nil
}
func (r *openAPIA2Cache) IncrementCreateCount(context.Context, int64, time.Duration) (int64, error) {
	if r.fail {
		return 0, errors.New("synthetic cache failure")
	}
	r.creates++
	return r.creates, nil
}
func (r *openAPIA2Cache) DeleteAuthCache(context.Context, string) error {
	r.invalidations++
	return nil
}
func (r *openAPIA2Cache) PublishAuthCacheInvalidation(context.Context, string) error { return nil }

type openAPIA2Idempotency struct {
	service.IdempotencyRepository
	mu           sync.Mutex
	record       *service.IdempotencyRecord
	fail         bool
	forcedStatus string
}

func (r *openAPIA2Idempotency) CreateProcessing(_ context.Context, v *service.IdempotencyRecord) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return false, errors.New("synthetic idempotency unavailable")
	}
	if r.record != nil {
		return false, nil
	}
	v.ID = 1
	c := *v
	r.record = &c
	if r.forcedStatus != "" {
		r.record.Status = r.forcedStatus
		return false, nil
	}
	return true, nil
}
func (r *openAPIA2Idempotency) GetByScopeAndKeyHash(context.Context, string, string) (*service.IdempotencyRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.record == nil {
		return nil, nil
	}
	c := *r.record
	return &c, nil
}
func (r *openAPIA2Idempotency) MarkSucceeded(_ context.Context, _ int64, status int, body string, expires time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record.Status = service.IdempotencyStatusSucceeded
	r.record.ResponseStatus = &status
	r.record.ResponseBody = &body
	r.record.ExpiresAt = expires
	return nil
}
func (r *openAPIA2Idempotency) MarkFailedRetryable(_ context.Context, _ int64, reason string, locked, expires time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.record.Status = service.IdempotencyStatusFailedRetryable
	r.record.ErrorReason = &reason
	r.record.LockedUntil = &locked
	r.record.ExpiresAt = expires
	return nil
}
func (r *openAPIA2Idempotency) ExtendProcessingLock(context.Context, int64, string, time.Time, time.Time) (bool, error) {
	return true, nil
}

type openAPIA2Settings struct{ *openAPIA1Settings }

func (r *openAPIA2Settings) SetMultiple(_ context.Context, values map[string]string) error {
	for k, v := range values {
		r.values[k] = v
	}
	return nil
}

type openAPIA2Fixture struct {
	a1                    *openAPIA1Fixture
	keys                  *openAPIA2Keys
	groups                *openAPIA2Groups
	subs                  *openAPIA2Subscriptions
	rates                 *openAPIA2Rates
	cache                 *openAPIA2Cache
	idem                  *openAPIA2Idempotency
	enforceIdem           bool
	maxActive, maxCreates int
}

func openAPIA2NewFixture(t *testing.T) *openAPIA2Fixture {
	a := openAPIA1NewFixture(t)
	a.users.user.Role = service.RoleUser
	now := time.Now().UTC()
	a.users.user.LastActiveAt = &now
	a.users.user.AllowedGroups = []int64{13}
	delete(a.settings.values, "admin_compliance_acknowledgement:41")
	gs := []service.Group{{ID: 11, Name: "public", Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 1.25}, {ID: 12, Name: "exclusive hidden", IsExclusive: true, Status: service.StatusActive}, {ID: 13, Name: "exclusive allowed", IsExclusive: true, Status: service.StatusActive}, {ID: 14, Name: "subscribed", SubscriptionType: service.SubscriptionTypeSubscription, Status: service.StatusActive}, {ID: 15, Name: "unsubscribed", SubscriptionType: service.SubscriptionTypeSubscription, Status: service.StatusActive}}
	gs[0].I18n = domain.GroupI18n{"en": {Name: "localized public"}}
	gs[0].VideoModelPrices = map[string]map[string]float64{"fixture-video": {"720p": 0.125}}
	gs[0].ReasoningEffortMappings = []domain.ReasoningEffortMapping{{From: "high", To: "medium", MatchType: "prefix", Model: "fixture"}}
	own := service.APIKey{ID: 101, UserID: 41, Key: openAPIA2OwnKey, Name: "owner", Status: service.StatusActive, GroupID: &gs[0].ID, Group: &gs[0], User: a.users.user, Quota: 12.5, QuotaUsed: 2.5, RateLimit5h: 3, Window5hStart: &now, Usage5h: 1.25, CreatedAt: now, UpdatedAt: now, IPWhitelist: []string{"192.0.2.1"}}
	other := own
	other.ID = 102
	other.UserID = 42
	other.Key = openAPIA2OtherKey
	return &openAPIA2Fixture{a1: a, keys: &openAPIA2Keys{keys: map[int64]service.APIKey{101: own, 102: other}}, groups: &openAPIA2Groups{groups: gs}, subs: &openAPIA2Subscriptions{groups: map[int64]bool{14: true}}, rates: &openAPIA2Rates{rates: map[int64]float64{11: 0.125, 13: 2}}, cache: &openAPIA2Cache{}, idem: &openAPIA2Idempotency{}}
}
func (f *openAPIA2Fixture) router(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = openAPIA1SigningKey
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Gateway.MaxBodySize = 1 << 20
	cfg.Gateway.TextMaxBodySize = 1 << 20
	cfg.APIKeyCreate.MaxActivePerUser = f.maxActive
	cfg.APIKeyCreate.MaxPerUserPerHour = f.maxCreates
	repo := &openAPIA2Settings{f.a1.settings}
	settings := service.NewSettingService(repo, cfg)
	// Use the production setter to update its process cache; mutating a repository
	// alone is not enough. Preserve per-case unrelated fixture settings.
	saved := map[string]string{}
	for k, v := range repo.values {
		saved[k] = v
	}
	if err := settings.UpdateSettings(context.Background(), &service.SystemSettings{BackendModeEnabled: saved[service.SettingKeyBackendModeEnabled] == "true"}); err != nil {
		t.Fatal(err)
	}
	repo.values = saved
	t.Cleanup(func() {
		if err := settings.UpdateSettings(context.Background(), &service.SystemSettings{}); err != nil {
			t.Error(err)
		}
	})
	users := service.NewUserService(f.a1.users, f.a1.settings, nil, nil)
	auth := service.NewAuthService(nil, f.a1.users, nil, f.a1.refresh, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	h := openAPIHandlers(t)
	h.APIKey = handler.NewAPIKeyHandler(service.NewAPIKeyService(f.keys, f.a1.users, f.groups, f.subs, f.rates, f.cache, cfg))
	icfg := service.DefaultIdempotencyConfig()
	icfg.ObserveOnly = !f.enforceIdem
	previous := service.DefaultIdempotencyCoordinator()
	service.SetDefaultIdempotencyCoordinator(service.NewIdempotencyCoordinator(f.idem, icfg))
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(previous) })
	client := redis.NewClient(&redis.Options{Addr: "openapi-fixture.invalid:0", MaxRetries: -1})
	client.AddHook(f.a1.counter)
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Error(err)
		}
	})
	forbidden := func(c *gin.Context) { t.Error("unrelated auth seam reached"); c.AbortWithStatus(599) }
	router := gin.New()
	server.SetupRouter(router, h, middleware.NewJWTAuthMiddleware(auth, users, settings, nil), middleware.OptionalJWTAuthMiddleware(forbidden), middleware.AdminAuthMiddleware(forbidden), middleware.APIKeyAuthMiddleware(forbidden), middleware.NewAuditLogMiddleware(nil), middleware.StepUpAuthMiddleware(forbidden), nil, nil, nil, settings, nil, cfg, client)
	t.Cleanup(func() { middleware.SetIngressRejectRecorder(nil) })
	return router
}
func (f *openAPIA2Fixture) limit() {
	f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"public_ip_rpm":1,"exempt_admin":false}`
	f.a1.counter.count = 2
}
func openAPIA2RepoFailure(f *openAPIA2Fixture) {
	f.keys.fail = true
	f.groups.fail = true
	f.rates.fail = true
}
