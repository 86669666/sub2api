package routes_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"testing"
	"time"
)

// These repositories capture production arguments. They do not prove SQL filtering.
type openAPIA3Usage struct {
	service.UsageLogRepository
	record              service.UsageLog
	empty, fail         bool
	calls               int
	filters             usagestats.UsageLogFilters
	params              pagination.PaginationParams
	start, end          time.Time
	granularity, source string
	batch               []int64
	userID, keyID       int64
}

func (r *openAPIA3Usage) hit() error {
	r.calls++
	if r.fail {
		return errors.New("synthetic usage repository unavailable")
	}
	return nil
}
func (r *openAPIA3Usage) GetByID(_ context.Context, id int64) (*service.UsageLog, error) {
	if err := r.hit(); err != nil {
		return nil, err
	}
	if id != 101 && id != 102 {
		return nil, service.ErrUsageLogNotFound
	}
	v := r.record
	v.ID = id
	if id == 102 {
		v.UserID = 42
	}
	return &v, nil
}
func (r *openAPIA3Usage) ListWithFilters(_ context.Context, p pagination.PaginationParams, f usagestats.UsageLogFilters) ([]service.UsageLog, *pagination.PaginationResult, error) {
	r.params = p
	r.filters = f
	if err := r.hit(); err != nil {
		return nil, nil, err
	}
	rows := []service.UsageLog{r.record}
	if r.empty {
		rows = nil
	}
	return rows, &pagination.PaginationResult{Total: int64(len(rows)), Page: p.Page, PageSize: p.PageSize}, nil
}
func (r *openAPIA3Usage) GetStatsWithFilters(_ context.Context, f usagestats.UsageLogFilters) (*usagestats.UsageStats, error) {
	r.filters = f
	if err := r.hit(); err != nil {
		return nil, err
	}
	s := &usagestats.UsageStats{}
	if !r.empty {
		x := 9.25
		s.TotalRequests = 2
		s.TotalActualCost = .125
		s.TotalAccountCost = &x
		s.Endpoints = []usagestats.EndpointStat{{Endpoint: "/v1/messages", Requests: 2, Cost: .25}}
		s.UpstreamEndpoints = []usagestats.EndpointStat{{Endpoint: "INTERNAL-CANARY"}}
		s.EndpointPaths = s.UpstreamEndpoints
	}
	return s, nil
}
func (r *openAPIA3Usage) GetUserDashboardStats(_ context.Context, id int64) (*usagestats.UserDashboardStats, error) {
	r.userID = id
	if err := r.hit(); err != nil {
		return nil, err
	}
	s := &usagestats.UserDashboardStats{}
	if !r.empty {
		s.TotalAPIKeys = 1
		s.TotalRequests = 2
		s.TotalActualCost = .125
		s.ByPlatform = []usagestats.PlatformDashboardStats{{Platform: "openai", TotalRequests: 2, TotalActualCost: .125}}
	}
	return s, nil
}
func (r *openAPIA3Usage) trend() ([]usagestats.TrendDataPoint, error) {
	if err := r.hit(); err != nil {
		return nil, err
	}
	if r.empty {
		return nil, nil
	}
	return []usagestats.TrendDataPoint{{Date: "2035-01-02", Requests: 2, InputTokens: 7, OutputTokens: 3, CacheCreationTokens: 5, CacheReadTokens: 4, TotalTokens: 19, Cost: .25, ActualCost: .125}}, nil
}
func (r *openAPIA3Usage) GetUsageTrendWithUsageFilters(_ context.Context, start, end time.Time, g string, f usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.filters = f
	r.start = start
	r.end = end
	r.granularity = g
	return r.trend()
}
func (r *openAPIA3Usage) GetUsageTrendWithFilters(_ context.Context, start, end time.Time, g string, user, key, account, group int64, model string, rt *int16, stream *bool, bt *int8) ([]usagestats.TrendDataPoint, error) {
	r.start = start
	r.end = end
	r.granularity = g
	r.userID = user
	r.keyID = key
	return r.trend()
}
func (r *openAPIA3Usage) GetModelStatsWithUsageFiltersBySource(_ context.Context, start, end time.Time, f usagestats.UsageLogFilters, source string) ([]usagestats.ModelStat, error) {
	r.start = start
	r.end = end
	r.filters = f
	r.source = source
	if err := r.hit(); err != nil {
		return nil, err
	}
	if r.empty {
		return nil, nil
	}
	return []usagestats.ModelStat{{Model: "requested", Requests: 2, Cost: .25, ActualCost: .125, AccountCost: 9.25}}, nil
}
func (r *openAPIA3Usage) GetGroupStatsWithUsageFilters(_ context.Context, start, end time.Time, f usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	r.start = start
	r.end = end
	r.filters = f
	if err := r.hit(); err != nil {
		return nil, err
	}
	if r.empty {
		return nil, nil
	}
	return []usagestats.GroupStat{{GroupID: 11, GroupName: "public", Requests: 2, Cost: .25, ActualCost: .125, AccountCost: 9.25}}, nil
}
func (r *openAPIA3Usage) GetBatchAPIKeyUsageStats(_ context.Context, ids []int64, start, end time.Time) (map[int64]*usagestats.BatchAPIKeyUsageStats, error) {
	r.batch = append([]int64(nil), ids...)
	r.start = start
	r.end = end
	if err := r.hit(); err != nil {
		return nil, err
	}
	if r.empty {
		return nil, nil
	}
	out := map[int64]*usagestats.BatchAPIKeyUsageStats{}
	for _, id := range ids {
		out[id] = &usagestats.BatchAPIKeyUsageStats{APIKeyID: id, TodayActualCost: .125, TotalActualCost: .25}
	}
	return out, nil
}

type openAPIA3Keys struct {
	*openAPIA2Keys
	verifyUser int64
	verifyIDs  []int64
}

func (r *openAPIA3Keys) VerifyOwnership(_ context.Context, user int64, ids []int64) ([]int64, error) {
	r.verifyUser = user
	r.verifyIDs = append([]int64(nil), ids...)
	if r.fail {
		return nil, errors.New("synthetic ownership store unavailable")
	}
	out := []int64{}
	for _, id := range ids {
		if k, ok := r.keys[id]; ok && k.UserID == user {
			out = append(out, id)
		}
	}
	return out, nil
}

type openAPIA3Ops struct {
	service.OpsRepository
	detail      service.OpsErrorLogDetail
	filter      *service.OpsErrorLogFilter
	calls       int
	fail, empty bool
}

func (r *openAPIA3Ops) ListErrorLogs(_ context.Context, f *service.OpsErrorLogFilter) (*service.OpsErrorLogList, error) {
	r.calls++
	x := *f
	r.filter = &x
	if r.fail {
		return nil, errors.New("synthetic error store unavailable")
	}
	rows := []*service.OpsErrorLog{&r.detail.OpsErrorLog}
	if r.empty || (f.APIKeyID != nil && *f.APIKeyID != 101) {
		rows = nil
	}
	return &service.OpsErrorLogList{Errors: rows, Total: len(rows), Page: f.Page, PageSize: f.PageSize}, nil
}
func (r *openAPIA3Ops) GetErrorLogByID(_ context.Context, id int64) (*service.OpsErrorLogDetail, error) {
	r.calls++
	if r.fail {
		return nil, errors.New("synthetic error store unavailable")
	}
	if id != 101 && id != 102 && id != 103 {
		return nil, sql.ErrNoRows
	}
	x := r.detail
	x.ID = id
	if id == 102 {
		u := int64(42)
		x.UserID = &u
	}
	if id == 103 {
		x.UserID = nil
	}
	return &x, nil
}

type openAPIA3Fixture struct {
	base  *openAPIA2Fixture
	usage *openAPIA3Usage
	keys  *openAPIA3Keys
	ops   *openAPIA3Ops
	noOps bool
}

func openAPIA3NewFixture(t *testing.T) *openAPIA3Fixture {
	b := openAPIA2NewFixture(t)
	b.a1.settings.values[service.SettingKeyAllowUserViewErrorRequests] = "true"
	stamp := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	uid := int64(41)
	key := int64(101)
	rt := int16(2)
	status := 503
	ip := "192.0.2.4"
	str := "synthetic"
	effort := "high"
	up := "INTERNAL-CANARY"
	dur := 25
	g := int64(11)
	row := service.UsageLog{ID: 101, UserID: 41, APIKeyID: 101, AccountID: 7, RequestID: "synthetic-usage", Model: "mapped-internal", RequestedModel: "requested", UpstreamModel: &up, UpstreamEndpoint: &up, RequestedReasoningEffort: &effort, UserAgent: &str, IPAddress: &ip, ServiceTier: &str, InboundEndpoint: &str, SessionID: &str, GroupID: &g, InputTokens: 7, OutputTokens: 3, TotalCost: .25, ActualCost: .125, RateMultiplier: .5, DurationMs: &dur, FirstTokenMs: &dur, ImageSizeBreakdown: map[string]int{"1024x1024": 1}, CreatedAt: stamp, User: b.a1.users.user}
	return openAPIA3Build(b, row, uid, key, rt, status, ip, stamp)
}
func openAPIA3Build(b *openAPIA2Fixture, row service.UsageLog, uid, key int64, rt int16, status int, ip string, stamp time.Time) *openAPIA3Fixture {
	k := b.keys.keys[101]
	row.APIKey = &k
	row.Group = &b.groups.groups[0]
	row.Subscription = &service.UserSubscription{ID: 9, UserID: 41, GroupID: 11, StartsAt: stamp, ExpiresAt: stamp.AddDate(1, 0, 0), Status: "active", CreatedAt: stamp, UpdatedAt: stamp, User: b.a1.users.user, Group: row.Group}
	f := &openAPIA3Fixture{base: b, usage: &openAPIA3Usage{record: row}, keys: &openAPIA3Keys{openAPIA2Keys: b.keys}, ops: &openAPIA3Ops{detail: service.OpsErrorLogDetail{OpsErrorLog: service.OpsErrorLog{ID: 101, UserID: &uid, APIKeyID: &key, CreatedAt: stamp, Model: "mapped-internal", RequestedModel: "requested", Phase: "upstream", Type: "api_error", StatusCode: 502, Platform: "openai", Message: "synthetic failure", APIKeyName: "owner", UserEmail: "INTERNAL-CANARY", ClientIP: &ip, RequestType: &rt, GroupName: "public", UserAgent: "synthetic", InboundEndpoint: "/v1/messages"}, ErrorBody: `{"error":"synthetic failure"}`, APIKeyPrefix: "INTERNAL-CANARY", UpstreamStatusCode: &status}}}
	b.configure = func(h *handler.Handlers, cfg *config.Config, settings *service.SettingService) {
		var ops *service.OpsService
		if !f.noOps {
			ops = service.NewOpsService(f.ops, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
		}
		h.Usage = handler.NewUsageHandler(service.NewUsageService(f.usage, b.a1.users, nil, nil), service.NewAPIKeyService(f.keys, b.a1.users, b.groups, b.subs, b.rates, b.cache, cfg), ops, settings)
	}
	return f
}
func (f *openAPIA3Fixture) router(t *testing.T) *gin.Engine { return f.base.router(t) }
