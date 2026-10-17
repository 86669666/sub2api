//go:build unit

package routes_test

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var openAPIA9Columns = []string{"id", "name", "description", "status", "model_mapping", "billing_model_source", "restrict_models", "features", "features_config", "apply_pricing_to_account_stats", "created_at", "updated_at"}

func TestOpenAPIA9RepositoryPagination(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, tc := range []struct {
		name, query, order string
		limit, offset      int
	}{
		{"default", "", "c.created_at DESC, c.id DESC", 20, 0},
		{"upper limit", "?page=2&page_size=1000", "c.created_at DESC, c.id DESC", 1000, 1000},
		{"out of range default", "?page=0&page_size=1001", "c.created_at DESC, c.id DESC", 20, 0},
		{"alias and first value", "?page=0002&page=9&page_size=&limit=3&sort_by=%20NAME%20&sort_order=%20ASC%20", "c.name ASC, c.id ASC", 3, 3},
		{"invalid size blocks alias", "?page_size=bad&limit=3&sort_by=garbage&sort_order=desc", "c.id ASC, c.id ASC", 20, 0},
		{"invalid order defaults asc", "?sort_by=status&sort_order=invalid", "c.status ASC, c.id ASC", 20, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, m, e := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = db.Close() })
			m.ExpectQuery(`SELECT COUNT\(\*\) FROM channels c WHERE 1=1`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			m.ExpectQuery(`SELECT c.id,.* FROM channels c WHERE 1=1 ORDER BY `+tc.order+` LIMIT \$1 OFFSET \$2`).WithArgs(tc.limit, tc.offset).WillReturnRows(sqlmock.NewRows(openAPIA9Columns))
			f, b := openAPIA9Run(t, "get", "/api/v1/admin/channels", doc.Paths["/api/v1/admin/channels"]["get"], openAPIA9Case{status: 200, query: tc.query, change: func(f *openAPIA9Fixture) { f.repoOverride = repository.NewChannelRepository(db) }})
			_ = f
			if len(openAPIA5Array(t, openAPIA5Map(t, b["data"])["items"])) != 0 {
				t.Fatal("empty list not array")
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
	t.Run("literal search and exact status", func(t *testing.T) {
		db, m, e := sqlmock.New()
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = db.Close() })
		m.ExpectQuery(`SELECT COUNT.*c.status = \$1 AND \(c.name ILIKE \$2 OR c.description ILIKE \$2\)`).WithArgs(" active ", `%50\%\_off%`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		m.ExpectQuery(`SELECT c.id,.*c.status = \$1 AND \(c.name ILIKE \$2 OR c.description ILIKE \$2\).*LIMIT \$3 OFFSET \$4`).WithArgs(" active ", `%50\%\_off%`, 20, 0).WillReturnRows(sqlmock.NewRows(openAPIA9Columns))
		openAPIA9Run(t, "get", "/api/v1/admin/channels", doc.Paths["/api/v1/admin/channels"]["get"], openAPIA9Case{status: 200, query: "?status=%20active%20&search=%2050%25_off%20", change: func(f *openAPIA9Fixture) { f.repoOverride = repository.NewChannelRepository(db) }})
		if e = m.ExpectationsWereMet(); e != nil {
			t.Fatal(e)
		}
	})
}
func TestOpenAPIA9RepositoryDelete(t *testing.T) {
	doc, _ := openAPILoad(t)
	path := "/api/v1/admin/channels/{id}"
	for _, count := range []int64{0, 1} {
		t.Run(map[bool]string{true: "exists", false: "missing"}[count == 1], func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = db.Close() })
			m.ExpectQuery(`SELECT group_id FROM channel_groups WHERE channel_id = \$1 ORDER BY group_id`).WithArgs(301).WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
			m.ExpectExec(`DELETE FROM channels WHERE id = \$1`).WithArgs(301).WillReturnResult(sqlmock.NewResult(0, count))
			want := 404
			if count == 1 {
				want = 200
				m.ExpectQuery(`SELECT id,.* FROM channels ORDER BY id`).WillReturnRows(sqlmock.NewRows(openAPIA9Columns))
			}
			openAPIA9Run(t, "delete", path, doc.Paths[path]["delete"], openAPIA9Case{status: want, change: func(f *openAPIA9Fixture) { f.repoOverride = repository.NewChannelRepository(db) }})
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestOpenAPIA9RepositoryTransactions(t *testing.T) {
	for _, op := range []string{"create", "update"} {
		for _, kind := range []string{"commit", "unique", "storage", "commit failure"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				db, m, e := sqlmock.New()
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { _ = db.Close() })
				repo := repository.NewChannelRepository(db)
				c := &service.Channel{ID: 301, Name: "Synthetic", Status: "active", BillingModelSource: "channel_mapped"}
				m.ExpectBegin()
				var cause error
				if kind == "unique" {
					cause = &pq.Error{Code: "23505"}
				} else if kind == "storage" {
					cause = errors.New("synthetic write failure")
				}
				if op == "create" {
					q := m.ExpectQuery(`INSERT INTO channels .* RETURNING id, created_at, updated_at`).WithArgs("Synthetic", "", "active", []byte("{}"), "channel_mapped", false, "", []byte("{}"), false)
					if cause != nil {
						q.WillReturnError(cause)
					} else {
						at := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
						q.WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(301, at, at))
					}
				} else {
					q := m.ExpectExec(`UPDATE channels SET .* WHERE id = \$10`).WithArgs("Synthetic", "", "active", []byte("{}"), "channel_mapped", false, "", []byte("{}"), false, 301)
					if cause != nil {
						q.WillReturnError(cause)
					} else {
						q.WillReturnResult(sqlmock.NewResult(0, 1))
					}
				}
				if cause != nil {
					m.ExpectRollback()
				} else if kind == "commit failure" {
					m.ExpectCommit().WillReturnError(errors.New("synthetic commit failure"))
				} else {
					m.ExpectCommit()
				}
				if op == "create" {
					e = repo.Create(context.Background(), c)
				} else {
					e = repo.Update(context.Background(), c)
				}
				if (e == nil) != (kind == "commit") {
					t.Fatalf("unexpected transaction result: %v", e)
				}
				if kind == "unique" && !errors.Is(e, service.ErrChannelExists) {
					t.Fatalf("unique not mapped: %v", e)
				}
				if e = m.ExpectationsWereMet(); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
func TestOpenAPIA9RepositoryNotFound(t *testing.T) {
	doc, _ := openAPILoad(t)
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	m.ExpectQuery(`SELECT id,.*FROM channels WHERE id = \$1`).WithArgs(-1).WillReturnError(sql.ErrNoRows)
	openAPIA9Run(t, "get", "/api/v1/admin/channels/{id}", doc.Paths["/api/v1/admin/channels/{id}"]["get"], openAPIA9Case{id: "-1", status: 404, change: func(f *openAPIA9Fixture) { f.repoOverride = repository.NewChannelRepository(db) }})
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

// A successful commit followed by reread failure is an error response with a
// persisted mutation. This prevents the contract tests asserting false rollback.
func TestOpenAPIA9PostCommitReadFailure(t *testing.T) {
	doc, _ := openAPILoad(t)
	f := openAPIA9NewFixture(t)
	f.repo.fail = "get"
	r := f.router(t)
	req := httptest.NewRequest("POST", "/api/v1/admin/channels", strings.NewReader(`{"name":"Created"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 500 || f.repo.writes != 1 {
		t.Fatalf("post-commit error drift: %d writes=%d", rec.Code, f.repo.writes)
	}
	if e := openAPIValidateResponse(doc.Paths["/api/v1/admin/channels"]["post"], rec); e != nil {
		t.Fatal(e)
	}
}

func TestOpenAPIA9RepositoryClearAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{true: "rollback child failure", false: "clear commits"}[fail], func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = db.Close() })
			repo := repository.NewChannelRepository(db)
			c := &service.Channel{ID: 301, Name: "Synthetic", GroupIDs: []int64{}, ModelPricing: []service.ChannelModelPricing{}, AccountStatsPricingRules: []service.AccountStatsPricingRule{}}
			m.ExpectBegin()
			m.ExpectExec(`UPDATE channels SET .* WHERE id = \$10`).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec(`DELETE FROM channel_groups WHERE channel_id = \$1`).WithArgs(301).WillReturnResult(sqlmock.NewResult(0, 1))
			q := m.ExpectExec(`DELETE FROM channel_model_pricing WHERE channel_id = \$1`).WithArgs(301)
			if fail {
				q.WillReturnError(errors.New("synthetic child failure"))
				m.ExpectRollback()
			} else {
				q.WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec(`DELETE FROM channel_account_stats_pricing_rules WHERE channel_id = \$1`).WithArgs(301).WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit()
			}
			e = repo.Update(context.Background(), c)
			if (e != nil) != fail {
				t.Fatalf("error=%v", e)
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
