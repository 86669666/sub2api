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
	"testing"
	"time"
)

// The repositories implement the persistence boundary in memory. SQL filtering,
// transactions, unique constraints and Redis Lua are not executed by this fixture.
type openAPIA6Announcements struct {
	service.AnnouncementRepository
	rows          map[int64]service.Announcement
	calls, writes int
	fail          string
	params        pagination.PaginationParams
	filters       service.AnnouncementListFilters
}

func (r *openAPIA6Announcements) err(op string) error {
	r.calls++
	if r.fail == op || r.fail == "all" {
		return fmt.Errorf("PRIVATE-A6-REPOSITORY")
	}
	return nil
}
func (r *openAPIA6Announcements) Create(_ context.Context, a *service.Announcement) error {
	if e := r.err("create"); e != nil {
		return e
	}
	a.ID = 901
	a.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a.UpdatedAt = a.CreatedAt
	r.rows[a.ID] = *a
	r.writes++
	return nil
}
func (r *openAPIA6Announcements) GetByID(_ context.Context, id int64) (*service.Announcement, error) {
	if e := r.err("get"); e != nil {
		return nil, e
	}
	a, ok := r.rows[id]
	if !ok {
		return nil, service.ErrAnnouncementNotFound
	}
	return &a, nil
}
func (r *openAPIA6Announcements) Update(_ context.Context, a *service.Announcement) error {
	if e := r.err("update"); e != nil {
		return e
	}
	r.rows[a.ID] = *a
	r.writes++
	return nil
}
func (r *openAPIA6Announcements) Delete(_ context.Context, id int64) error {
	if e := r.err("delete"); e != nil {
		return e
	}
	if _, ok := r.rows[id]; ok {
		delete(r.rows, id)
		r.writes++
	}
	return nil
}
func (r *openAPIA6Announcements) all() []service.Announcement {
	out := []service.Announcement{}
	for _, a := range r.rows {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}
func (r *openAPIA6Announcements) List(_ context.Context, p pagination.PaginationParams, f service.AnnouncementListFilters) ([]service.Announcement, *pagination.PaginationResult, error) {
	r.params = p
	r.filters = f
	if e := r.err("list"); e != nil {
		return nil, nil, e
	}
	return r.all(), &pagination.PaginationResult{Total: int64(len(r.rows))}, nil
}
func (r *openAPIA6Announcements) ListActive(_ context.Context, _ time.Time) ([]service.Announcement, error) {
	if e := r.err("active"); e != nil {
		return nil, e
	}
	return r.all(), nil
} // Deliberately overreturns; real service must recheck visibility.
type openAPIA6Reads struct {
	service.AnnouncementReadRepository
	values                  map[[2]int64]time.Time
	fail                    string
	calls, writes, attempts int
	user, ann               int64
	ids                     []int64
}

func (r *openAPIA6Reads) MarkRead(_ context.Context, a, u int64, at time.Time) error {
	r.calls++
	r.attempts++
	r.ann = a
	r.user = u
	if r.fail == "mark" {
		return fmt.Errorf("PRIVATE-A6-READ")
	}
	key := [2]int64{a, u}
	if _, ok := r.values[key]; !ok {
		r.values[key] = at
		r.writes++
	}
	return nil
}
func (r *openAPIA6Reads) GetReadMapByUser(_ context.Context, u int64, ids []int64) (map[int64]time.Time, error) {
	r.calls++
	r.user = u
	r.ids = append([]int64{}, ids...)
	if r.fail == "user" {
		return nil, fmt.Errorf("PRIVATE-A6-READ")
	}
	out := map[int64]time.Time{}
	for _, id := range ids {
		if at, ok := r.values[[2]int64{id, u}]; ok {
			out[id] = at
		}
	}
	return out, nil
}
func (r *openAPIA6Reads) GetReadMapByUsers(_ context.Context, a int64, ids []int64) (map[int64]time.Time, error) {
	r.calls++
	r.ann = a
	r.ids = append([]int64{}, ids...)
	if r.fail == "users" {
		return nil, fmt.Errorf("PRIVATE-A6-READ")
	}
	out := map[int64]time.Time{}
	for _, id := range ids {
		if at, ok := r.values[[2]int64{a, id}]; ok {
			out[id] = at
		}
	}
	return out, nil
}

type openAPIA6Users struct {
	*openAPIA1Users
	list    []service.User
	fail    string
	calls   int
	params  pagination.PaginationParams
	filters service.UserListFilters
}

func (u *openAPIA6Users) GetByID(ctx context.Context, id int64) (*service.User, error) {
	u.calls++
	if u.fail == "get" && u.calls > 1 {
		return nil, fmt.Errorf("PRIVATE-A6-USER")
	}
	return u.openAPIA1Users.GetByID(ctx, id)
}
func (u *openAPIA6Users) ListWithFilters(_ context.Context, p pagination.PaginationParams, f service.UserListFilters) ([]service.User, *pagination.PaginationResult, error) {
	u.params = p
	u.filters = f
	if u.fail == "list" {
		return nil, nil, fmt.Errorf("PRIVATE-A6-USER")
	}
	return u.list, &pagination.PaginationResult{Total: int64(len(u.list))}, nil
}

type openAPIA6Subs struct {
	service.UserSubscriptionRepository
	groups map[int64][]int64
	ids    []int64
	fail   bool
}

func (s *openAPIA6Subs) ListActiveByUserID(_ context.Context, id int64) ([]service.UserSubscription, error) {
	s.ids = append(s.ids, id)
	if s.fail {
		return nil, fmt.Errorf("PRIVATE-A6-SUB")
	}
	out := []service.UserSubscription{}
	for _, g := range s.groups[id] {
		out = append(out, service.UserSubscription{UserID: id, GroupID: g})
	}
	return out, nil
}

type openAPIA6Audit struct {
	service.AuditLogRepository
	logs []*service.AuditLog
}

func (a *openAPIA6Audit) BatchInsert(_ context.Context, logs []*service.AuditLog) (int64, error) {
	a.logs = append(a.logs, logs...)
	return int64(len(logs)), nil
}

type openAPIA6Fixture struct {
	announcementRepo service.AnnouncementRepository
	a1               *openAPIA1Fixture
	anns             *openAPIA6Announcements
	reads            *openAPIA6Reads
	users            *openAPIA6Users
	subs             *openAPIA6Subs
	audit            *openAPIA6Audit
	auditService     *service.AuditLogService
}

func openAPIA6NewFixture(t *testing.T, adminRoute bool) *openAPIA6Fixture {
	a1 := openAPIA1NewFixture(t)
	activeAt := time.Now()
	a1.users.user.LastActiveAt = &activeAt
	a1.users.user.Balance = 10.5
	if !adminRoute {
		a1.users.user.Role = service.RoleUser
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	actor := int64(71)
	a := service.Announcement{ID: 301, Title: "Announcement", Content: "Content", Status: "active", NotifyMode: "popup", CreatedBy: &actor, UpdatedBy: &actor, CreatedAt: now, UpdatedAt: now}
	return &openAPIA6Fixture{a1: a1, anns: &openAPIA6Announcements{rows: map[int64]service.Announcement{301: a}}, reads: &openAPIA6Reads{values: map[[2]int64]time.Time{}}, users: &openAPIA6Users{openAPIA1Users: a1.users, list: []service.User{*a1.users.user, {ID: 42, Email: "other@example.invalid", Username: "other", Balance: 0}}}, subs: &openAPIA6Subs{groups: map[int64][]int64{41: {14}}}, audit: &openAPIA6Audit{}}
}
func (f *openAPIA6Fixture) router(t *testing.T) *gin.Engine {
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
	users := service.NewUserService(f.users, f.a1.settings, nil, nil)
	auth := service.NewAuthService(nil, f.users, nil, f.a1.refresh, cfg, settings, nil, nil, nil, nil, nil, nil, nil)
	repo := f.announcementRepo
	if repo == nil {
		repo = f.anns
	}
	svc := service.NewAnnouncementService(repo, f.reads, f.users, f.subs)
	h := openAPIHandlers(t)
	h.Announcement = handler.NewAnnouncementHandler(svc)
	h.Admin.Announcement = admin.NewAnnouncementHandler(svc)
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
