package routes_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Memory repositories capture the production service's scope and selected method.
// Filtering here is fixture behavior, not evidence for production SQL predicates.
type openAPIA4Subscriptions struct {
	service.UserSubscriptionRepository
	rows                   []service.UserSubscription
	listErr                bool
	getErrors              map[int64]bool
	listedUser             int64
	listCalls, activeCalls int
	gets                   []int64
}

func (r *openAPIA4Subscriptions) list(user int64, active bool) ([]service.UserSubscription, error) {
	r.listedUser = user
	if active {
		r.activeCalls++
	} else {
		r.listCalls++
	}
	if r.listErr {
		return nil, errors.New("synthetic subscription store unavailable")
	}
	var out []service.UserSubscription
	for _, v := range r.rows {
		if v.UserID == user && (!active || v.IsActive()) {
			out = append(out, v)
		}
	}
	return out, nil
}
func (r *openAPIA4Subscriptions) ListByUserID(_ context.Context, id int64) ([]service.UserSubscription, error) {
	return r.list(id, false)
}
func (r *openAPIA4Subscriptions) ListActiveByUserID(_ context.Context, id int64) ([]service.UserSubscription, error) {
	return r.list(id, true)
}
func (r *openAPIA4Subscriptions) GetByID(_ context.Context, id int64) (*service.UserSubscription, error) {
	r.gets = append(r.gets, id)
	if r.getErrors[id] {
		return nil, errors.New("synthetic per-item read failure")
	}
	for _, v := range r.rows {
		if v.ID == id {
			cp := v
			return &cp, nil
		}
	}
	return nil, service.ErrSubscriptionNotFound
}

type openAPIA4Groups struct {
	service.GroupRepository
	group service.Group
	fail  bool
	ids   []int64
}

func (r *openAPIA4Groups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	r.ids = append(r.ids, id)
	if r.fail {
		return nil, errors.New("synthetic group read failure")
	}
	v := r.group
	return &v, nil
}

type openAPIA4Fixture struct {
	base   *openAPIA2Fixture
	subs   *openAPIA4Subscriptions
	groups *openAPIA4Groups
}

func openAPIA4NewFixture(t *testing.T) *openAPIA4Fixture {
	t.Helper()
	now := time.Now().UTC()
	ten, twenty, forty := 10.0, 20.0, 40.0
	g := service.Group{ID: 13, Name: "Original", Description: "Original description", Status: service.StatusActive, SubscriptionType: "subscription", DailyLimitUSD: &ten, WeeklyLimitUSD: &twenty, MonthlyLimitUSD: &forty, I18n: service.GroupI18n{"en": {Name: "Translated", Description: "Translated description"}}}
	sub := service.UserSubscription{ID: 201, UserID: 41, GroupID: 13, StartsAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(10 * 24 * time.Hour), Status: service.SubscriptionStatusActive, DailyWindowStart: &now, WeeklyWindowStart: &now, MonthlyWindowStart: &now, DailyUsageUSD: 2.5, WeeklyUsageUSD: 5, MonthlyUsageUSD: 10, CreatedAt: now, UpdatedAt: now, Group: &g, Notes: "INTERNAL-CANARY"}
	other := sub
	other.ID = 202
	other.UserID = 42
	base := openAPIA2NewFixture(t)
	f := &openAPIA4Fixture{base: base, subs: &openAPIA4Subscriptions{rows: []service.UserSubscription{sub, other}, getErrors: map[int64]bool{}}, groups: &openAPIA4Groups{group: g}}
	base.configure = func(h *handler.Handlers, _ *config.Config, _ *service.SettingService) {
		// Nil config disables L1 and maintenance; nil billing cache prevents subscriber I/O.
		svc := service.NewSubscriptionService(f.groups, f.subs, nil, nil, nil)
		t.Cleanup(svc.Stop)
		h.Subscription = handler.NewSubscriptionHandler(svc)
	}
	return f
}
func (f *openAPIA4Fixture) router(t *testing.T) *gin.Engine { return f.base.router(t) }
