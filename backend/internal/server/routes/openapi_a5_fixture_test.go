package routes_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Only read methods are implemented. Embedded interfaces deliberately panic on
// accidental probe/scheduler/write calls; network is denied by the test runner.
type openAPIA5Legacy struct {
	service.ChannelMonitorRepository
	monitor                                                  service.ChannelMonitor
	latest                                                   []*service.ChannelMonitorLatest
	history                                                  []*service.ChannelMonitorHistoryEntry
	availability                                             []*service.ChannelMonitorAvailability
	empty, fail, aggregateFail, latestFail, availabilityFail bool
	calls                                                    int
	ids                                                      []int64
	windows                                                  []int
	timelineLimit                                            int
}

func (r *openAPIA5Legacy) ListEnabled(context.Context) ([]*service.ChannelMonitor, error) {
	r.calls++
	if r.fail {
		return nil, errors.New("synthetic legacy store unavailable")
	}
	if r.empty {
		return nil, nil
	}
	return []*service.ChannelMonitor{&r.monitor}, nil
}
func (r *openAPIA5Legacy) GetByID(_ context.Context, id int64) (*service.ChannelMonitor, error) {
	r.calls++
	r.ids = append(r.ids, id)
	if r.fail {
		return nil, errors.New("synthetic legacy read failure")
	}
	if id != 301 {
		return nil, service.ErrChannelMonitorNotFound
	}
	return &r.monitor, nil
}
func (r *openAPIA5Legacy) ListLatestPerModel(_ context.Context, id int64) ([]*service.ChannelMonitorLatest, error) {
	r.calls++
	r.ids = append(r.ids, id)
	if r.latestFail {
		return nil, errors.New("synthetic latest failure")
	}
	return r.latest, nil
}
func (r *openAPIA5Legacy) ComputeAvailability(_ context.Context, id int64, days int) ([]*service.ChannelMonitorAvailability, error) {
	r.calls++
	r.ids = append(r.ids, id)
	r.windows = append(r.windows, days)
	if r.availabilityFail {
		return nil, errors.New("synthetic availability failure")
	}
	return r.availability, nil
}
func (r *openAPIA5Legacy) ListLatestForMonitorIDs(_ context.Context, ids []int64) (map[int64][]*service.ChannelMonitorLatest, error) {
	r.calls++
	r.ids = append(r.ids, ids...)
	if r.aggregateFail {
		return nil, errors.New("synthetic aggregate latest failure")
	}
	return map[int64][]*service.ChannelMonitorLatest{301: r.latest}, nil
}
func (r *openAPIA5Legacy) ComputeAvailabilityForMonitors(_ context.Context, ids []int64, days int) (map[int64][]*service.ChannelMonitorAvailability, error) {
	r.calls++
	r.ids = append(r.ids, ids...)
	r.windows = append(r.windows, days)
	if r.aggregateFail {
		return nil, errors.New("synthetic aggregate availability failure")
	}
	return map[int64][]*service.ChannelMonitorAvailability{301: r.availability}, nil
}
func (r *openAPIA5Legacy) ListRecentHistoryForMonitors(_ context.Context, ids []int64, _ map[int64]string, limit int) (map[int64][]*service.ChannelMonitorHistoryEntry, error) {
	r.calls++
	r.ids = append(r.ids, ids...)
	r.timelineLimit = limit
	if r.aggregateFail {
		return nil, errors.New("synthetic aggregate history failure")
	}
	return map[int64][]*service.ChannelMonitorHistoryEntry{301: r.history}, nil
}

type openAPIA5V2 struct {
	service.ChannelMonitorV2Repository
	config                                                     service.ChannelMonitorV2Config
	configNil, configFail, fail, empty, missingSelf, manyUsers bool
	calls, configCalls                                         int
	filter                                                     service.ChannelMonitorV2Filter
	admin                                                      bool
	groupBy                                                    service.ChannelMonitorV2GroupBy
}

func (r *openAPIA5V2) GetConfig(context.Context) (*service.ChannelMonitorV2Config, error) {
	r.configCalls++
	if r.configFail {
		return nil, errors.New("synthetic config unavailable")
	}
	if r.configNil {
		return nil, nil
	}
	v := r.config
	v.Platforms = append([]service.ChannelMonitorV2PlatformConfig(nil), r.config.Platforms...)
	return &v, nil
}
func (r *openAPIA5V2) hit(f service.ChannelMonitorV2Filter, a bool) error {
	r.calls++
	r.filter = f
	r.admin = a
	if r.fail {
		return errors.New("synthetic passive read failure")
	}
	return nil
}

// Fixture selection demonstrates transmitted scope, not production SQL behavior.
func (r *openAPIA5V2) noRows() bool {
	if r.empty {
		return true
	}
	if r.filter.RestrictGroups && len(r.filter.AllowedGroupIDs) == 0 {
		return true
	}
	if len(r.filter.GroupIDs) > 0 {
		for _, id := range r.filter.GroupIDs {
			if id == 11 {
				return false
			}
		}
		return true
	}
	return false
}
func openAPIA5Metric() service.ChannelMonitorV2Metric {
	n := int64(120)
	avg := 125.5
	up := int64(9)
	return service.ChannelMonitorV2Metric{SuccessRequests: 90, ErrorRequests: 10, RequestCount: 100, InputTokens: 110, OutputTokens: 120, CacheCreationTokens: 130, CacheReadTokens: 140, TokenCount: 500, RPM: 2.5, TPM: 12.5, ErrorRate: .1, SuccessRate: .9, CacheRate: .5, CacheRateNumerator: 50, CacheRateDenominator: 100, TTFT: service.ChannelMonitorV2Latency{SampleCount: 100, P50Ms: &n, P90Ms: &n, P95Ms: &n, AvgMs: &avg}, Duration: service.ChannelMonitorV2Latency{SampleCount: 100, P50Ms: &n, P90Ms: &n, P95Ms: &n, AvgMs: &avg}, UpstreamAffectedRequests: &up, UpstreamAttemptCount: &up}
}
func openAPIA5Health() service.ChannelMonitorV2Health {
	return service.ChannelMonitorV2HealthFor(openAPIA5Metric())
}
func openAPIA5Coverage(f service.ChannelMonitorV2Filter) service.ChannelMonitorV2Coverage {
	return service.ChannelMonitorV2Coverage{RequestedStart: f.Start, RequestedEnd: f.End, CoverageStart: f.Start, DataThrough: f.End, ComputedAt: f.End, AggregationLagSeconds: 2, CoverageComplete: false, BucketSeconds: int(f.Bucket.Seconds()), Bootstrap: &service.ChannelMonitorV2Bootstrap{Active: true, ProgressPercent: 50, CoveredFrom: f.Start, TargetStart: f.Start.Add(-24 * time.Hour)}}
}
func (r *openAPIA5V2) GetDimensions(_ context.Context, f service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config) (*service.ChannelMonitorV2Dimensions, error) {
	if err := r.hit(f, false); err != nil {
		return nil, err
	}
	v := &service.ChannelMonitorV2Dimensions{Platforms: []service.ChannelMonitorV2Dimension{}, Groups: []service.ChannelMonitorV2GroupDimension{}, Models: []service.ChannelMonitorV2Dimension{}}
	if !r.noRows() {
		v.Platforms = append(v.Platforms, service.ChannelMonitorV2Dimension{Value: "openai", Label: "OpenAI", RequestCount: 100})
		v.Groups = append(v.Groups, service.ChannelMonitorV2GroupDimension{ID: 11, Name: "public", Platform: "openai", RequestCount: 100})
		v.Models = append(v.Models, service.ChannelMonitorV2Dimension{Value: "model-a", Label: "model-a", Platform: "openai", RequestCount: 100})
	}
	return v, nil
}
func (r *openAPIA5V2) GetSnapshot(_ context.Context, f service.ChannelMonitorV2Filter, c service.ChannelMonitorV2Config, a bool) (*service.ChannelMonitorV2Snapshot, error) {
	if err := r.hit(f, a); err != nil {
		return nil, err
	}
	v := &service.ChannelMonitorV2Snapshot{Trend: []service.ChannelMonitorV2TrendPoint{}}
	if !r.noRows() {
		v.Config = c
		v.Coverage = openAPIA5Coverage(f)
		v.Metrics = openAPIA5Metric()
		v.Health = openAPIA5Health()
		v.Trend = append(v.Trend, service.ChannelMonitorV2TrendPoint{BucketStart: f.Start, Metrics: openAPIA5Metric(), Health: openAPIA5Health()})
	}
	return v, nil
}
func (r *openAPIA5V2) GetModels(_ context.Context, f service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, a bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2ModelRow], error) {
	if err := r.hit(f, a); err != nil {
		return nil, err
	}
	v := &service.ChannelMonitorV2List[service.ChannelMonitorV2ModelRow]{Items: []service.ChannelMonitorV2ModelRow{}}
	if !r.noRows() {
		v.Coverage = openAPIA5Coverage(f)
		v.Items = append(v.Items, service.ChannelMonitorV2ModelRow{Platform: "openai", Model: "model-a", Metrics: openAPIA5Metric(), Health: openAPIA5Health()})
	}
	return v, nil
}
func (r *openAPIA5V2) GetMatrix(_ context.Context, f service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, g service.ChannelMonitorV2GroupBy, a bool) (*service.ChannelMonitorV2Matrix, error) {
	r.groupBy = g
	if err := r.hit(f, a); err != nil {
		return nil, err
	}
	v := &service.ChannelMonitorV2Matrix{GroupBy: g, Items: []service.ChannelMonitorV2MatrixRow{}}
	if !r.noRows() {
		v.Coverage = openAPIA5Coverage(f)
		id := int64(11)
		row := service.ChannelMonitorV2MatrixRow{Platform: "openai", Metrics: openAPIA5Metric(), Health: openAPIA5Health(), Buckets: []service.ChannelMonitorV2TrendPoint{{BucketStart: f.Start, Metrics: openAPIA5Metric(), Health: openAPIA5Health()}}}
		if g == service.ChannelMonitorV2GroupByPlatformGroup || g == service.ChannelMonitorV2GroupByPlatformGroupModel {
			row.GroupID = &id
			row.GroupName = "public"
		}
		if g == service.ChannelMonitorV2GroupByPlatformModel || g == service.ChannelMonitorV2GroupByPlatformGroupModel {
			row.Model = "model-a"
		}
		v.Items = append(v.Items, row)
	}
	return v, nil
}
func (r *openAPIA5V2) GetErrors(_ context.Context, f service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, a bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2ErrorRow], error) {
	if err := r.hit(f, a); err != nil {
		return nil, err
	}
	v := &service.ChannelMonitorV2List[service.ChannelMonitorV2ErrorRow]{Items: []service.ChannelMonitorV2ErrorRow{}, Coverage: openAPIA5Coverage(f)}
	if !r.noRows() {
		v.Items = append(v.Items, service.ChannelMonitorV2ErrorRow{Category: "timeout", Count: 10, Rate: .1, Ignored: true, Details: []service.ChannelMonitorV2ErrorDetail{{Platform: "openai", Model: "model-a", ErrorType: "upstream", StatusCode: 502, UpstreamStatusCode: 503, Message: "ADMIN-DETAIL-CANARY", Count: 10}}})
	}
	return v, nil
}
func (r *openAPIA5V2) GetUsers(_ context.Context, f service.ChannelMonitorV2Filter, _ service.ChannelMonitorV2Config, a bool) (*service.ChannelMonitorV2List[service.ChannelMonitorV2UserRow], error) {
	if err := r.hit(f, a); err != nil {
		return nil, err
	}
	v := &service.ChannelMonitorV2List[service.ChannelMonitorV2UserRow]{Items: []service.ChannelMonitorV2UserRow{}, Coverage: openAPIA5Coverage(f)}
	if !r.noRows() {
		count := 2
		if r.manyUsers {
			count = 13
		}
		for i := 0; i < count; i++ {
			id := int64(42 + i)
			if !r.missingSelf && i == count-1 {
				id = 41
			}
			v.Items = append(v.Items, service.ChannelMonitorV2UserRow{UserID: &id, Email: "OTHER-IDENTITY-CANARY@example.invalid", Username: "OTHER-IDENTITY-CANARY", Metrics: openAPIA5Metric()})
			if id == 41 {
				v.Items[i].Email = "self@example.invalid"
				v.Items[i].Username = "self"
			}
		}
	}
	return v, nil
}

type openAPIA5Fixture struct {
	base            *openAPIA2Fixture
	legacy          *openAPIA5Legacy
	v2              *openAPIA5V2
	noRuntimeReader bool
}

func openAPIA5NewFixture(t *testing.T, path string) *openAPIA5Fixture {
	t.Helper()
	base := openAPIA2NewFixture(t)
	now := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
	ms, ping := 123, 12
	balance := 9.5
	quota := &domain.MonitorQuotaSnapshot{Source: "usage", Success: true, Tiers: []domain.MonitorQuotaTier{{Window: "5h", Label: "requests", UsedPercent: 25, Used: 5, Limit: 20, ResetAt: now.Format(time.RFC3339)}}, Balance: &balance, Balances: []domain.MonitorBalance{{Currency: "USD", Balance: 9.5}}, Currency: "USD", PlanLevel: "synthetic", BalanceLow: true, CredentialInvalid: true, Error: "synthetic quota status", FetchedAt: now}
	legacy := &openAPIA5Legacy{monitor: service.ChannelMonitor{ID: 301, Name: "Synthetic monitor", Provider: "openai", GroupName: "shared", PrimaryModel: "model-a", ExtraModels: []string{"model-b"}, Enabled: true, APIKey: "PRIVATE-MONITOR-CANARY", Endpoint: "PRIVATE-MONITOR-CANARY", ExtraHeaders: map[string]string{"x-secret": "PRIVATE-MONITOR-CANARY"}}, latest: []*service.ChannelMonitorLatest{{Model: "model-a", Status: "operational", LatencyMs: &ms, PingLatencyMs: &ping, CheckedAt: now, Quota: quota}, {Model: "model-b", Status: "degraded", CheckedAt: now}}, history: []*service.ChannelMonitorHistoryEntry{{ID: 1, Model: "model-a", Status: "operational", LatencyMs: &ms, PingLatencyMs: &ping, Message: "PRIVATE-MONITOR-CANARY", CheckedAt: now}, {ID: 2, Model: "model-a", Status: "failed", CheckedAt: now.Add(-time.Minute)}}, availability: []*service.ChannelMonitorAvailability{{Model: "model-a", WindowDays: 7, TotalChecks: 10, OperationalChecks: 9, AvailabilityPct: 90, AvgLatencyMs: &ms}}}
	actor := int64(99)
	v2 := &openAPIA5V2{config: service.ChannelMonitorV2Config{Version: 2, Enabled: true, RefreshIntervalSeconds: 300, Platforms: []service.ChannelMonitorV2PlatformConfig{{Platform: "openai", Enabled: true, Models: []string{"PRIVATE-POLICY-CANARY"}}}, GroupIDs: []int64{11, 99}, HealthThresholds: service.DefaultChannelMonitorV2HealthThresholds(), IgnoredErrorCategories: []string{"PRIVATE-POLICY-CANARY"}, UpdatedAt: now, UpdatedBy: &actor}}
	f := &openAPIA5Fixture{base: base, legacy: legacy, v2: v2}
	base.a1.settings.values[service.SettingKeyChannelMonitorEnabled] = "true"
	mode := "v1"
	if stringsContainsV2(path) {
		mode = "v2"
	}
	base.a1.settings.values[service.SettingKeyChannelMonitorMode] = mode
	base.configure = func(h *handler.Handlers, cfg *config.Config, settings *service.SettingService) {
		legacySvc := service.NewChannelMonitorService(f.legacy, nil)
		h.ChannelMonitor = handler.NewChannelMonitorUserHandler(legacySvc, settings)
		v2svc := service.NewChannelMonitorV2Service(f.v2)
		if !f.noRuntimeReader {
			v2svc.SetRuntimeReader(settings)
		}
		keys := service.NewAPIKeyService(base.keys, base.a1.users, base.groups, base.subs, base.rates, base.cache, cfg)
		h.ChannelMonitorV2 = handler.NewChannelMonitorV2Handler(v2svc, keys)
	}
	return f
}
func stringsContainsV2(path string) bool {
	return strings.HasPrefix(path, "/api/v1/channel-monitor-v2/")
}
func (f *openAPIA5Fixture) router(t *testing.T) *gin.Engine { return f.base.router(t) }
