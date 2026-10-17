package routes_test

import (
	"context"
	"encoding/json"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http/httptest"
	"testing"
	"time"
)

// Capture the actual registered route's rejection, not a manually invented
// schema sample. This also distinguishes global panel from backend-mode errors.
func TestOpenAPIA6ActualUserRejections(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/announcements"}, {"POST", "/api/v1/announcements/{id}/read"}} {
		for _, status := range []int{403, 429} {
			t.Run(route.method+"/"+route.path+"/"+fmt.Sprint(status), func(t *testing.T) {
				f := openAPIA6NewFixture(t, false)
				if status == 403 {
					f.a1.settings.values[service.SettingKeyBackendModeEnabled] = "true"
				} else {
					f.a1.settings.values[service.SettingKeyPanelRateLimitSettings] = `{"enabled":true,"user_rpm":1,"public_ip_rpm":1,"exempt_admin":false}`
					f.a1.counter.count = 2
				}
				r := f.router(t)
				req := httptest.NewRequest(route.method, openAPIA2URL(route.path, "301"), nil)
				req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				t.Logf("actual status=%d Retry-After=%q body=%s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
				if rec.Code != status {
					t.Fatalf("status=%d want=%d", rec.Code, status)
				}
				var body map[string]any
				if err := openAPIDecode(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if status == 429 {
					if body["code"] != "RATE_LIMITED" || rec.Header().Get("Retry-After") != "2" {
						t.Fatal("global panel rejection drift")
					}
				} else if body["code"] != json.Number("403") {
					t.Fatal("backend-mode rejection drift")
				}
				method := "get"
				if route.method == "POST" {
					method = "post"
				}
				if err := openAPIValidateResponse(doc.Paths[route.path][method], rec); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// SQLmock exercises the real Ent SQL builder and repository; no PostgreSQL
// server executes the generated query. In particular this is not a SQL data
// filtering/limit integration test.
func TestOpenAPIA6ListActiveSQLContract(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := repository.NewAnnouncementRepository(client)
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT .* FROM "announcements" WHERE \("announcements"\."status" = \$1 AND \("announcements"\."starts_at" IS NULL OR "announcements"\."starts_at" <= \$2\)\) AND \("announcements"\."ends_at" IS NULL OR "announcements"\."ends_at" > \$3\) ORDER BY "announcements"\."id" DESC LIMIT 200`).WithArgs("active", now, now).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	rows, err := repo.ListActive(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatal("unexpected rows")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// The actual repository sits behind the same real SetupRouter/auth/audit chain
// as the other contract cases. RowsAffected is supplied by SQLmock, not a DB.
func TestOpenAPIA6DeleteSQLContract(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/api/v1/admin/announcements/{id}"]["delete"]
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	f := openAPIA6NewFixture(t, true)
	f.announcementRepo = repository.NewAnnouncementRepository(client)
	r := f.router(t)
	for _, c := range []struct {
		name         string
		id, affected int64
	}{{"missing first", 999, 0}, {"missing repeated", 999, 0}, {"existing", 301, 1}, {"deleted repeated", 301, 0}} {
		t.Run(c.name, func(t *testing.T) {
			mock.ExpectExec(`^DELETE FROM "announcements" WHERE "announcements"\."id" = \$1$`).WithArgs(c.id).WillReturnResult(sqlmock.NewResult(0, c.affected))
			req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/admin/announcements/%d", c.id), nil)
			req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != 200 {
				t.Fatalf("affected=%d status=%d body=%s", c.affected, rec.Code, rec.Body.String())
			}
			if err := openAPIValidateResponse(op, rec); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	f.auditService.Stop()
	if len(f.audit.logs) != 4 {
		t.Fatalf("audit logs=%d", len(f.audit.logs))
	}
	for _, a := range f.audit.logs {
		if a.ActorUserID == nil || *a.ActorUserID != 41 || a.StatusCode != 200 {
			t.Fatal("delete audit projection drift")
		}
	}
	// Keep the in-memory double honest against the zero-row production behavior.
	for i := 0; i < 2; i++ {
		if err := f.anns.Delete(context.Background(), 999); err != nil {
			t.Fatal(err)
		}
	}
	if f.anns.writes != 0 {
		t.Fatal("missing fixture delete changed a row")
	}
	for i := 0; i < 2; i++ {
		if err := f.anns.Delete(context.Background(), 301); err != nil {
			t.Fatal(err)
		}
	}
	if f.anns.writes != 1 {
		t.Fatal("repeat fixture delete changed more than one row")
	}
	if _, ok := op.Responses["404"]; ok {
		t.Fatal("missing-row DELETE404 is not a production response")
	}
}

func TestOpenAPIA6UserErrorNegatives(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, route := range []struct{ method, path string }{{"get", "/api/v1/announcements"}, {"post", "/api/v1/announcements/{id}/read"}} {
		t.Run(route.path, func(t *testing.T) {
			op := doc.Paths[route.path][route.method]
			for _, c := range []struct {
				name   string
				status int
				body   string
				retry  string
				valid  bool
			}{
				{"actual backend mode", 403, `{"code":403,"message":"Backend mode is active. User self-service is disabled."}`, "", true},
				{"actual global panel", 429, `{"code":"RATE_LIMITED","message":"Too many requests, please slow down and try again later"}`, "2", true},
				{"numeric 429 is not this middleware", 429, `{"code":429,"message":"fabricated"}`, "2", false},
				{"monitor reason not reachable", 403, `{"code":403,"message":"fabricated","reason":"CHANNEL_MONITOR_DISABLED"}`, "", false},
				{"monitor mode reason not reachable", 403, `{"code":403,"message":"fabricated","reason":"CHANNEL_MONITOR_MODE_MISMATCH"}`, "", false},
				{"extra rate metadata", 429, `{"code":"RATE_LIMITED","message":"fabricated","metadata":{}}`, "2", false},
				{"wrong string code", 429, `{"code":"CHANNEL_MONITOR_DISABLED","message":"fabricated"}`, "2", false},
				{"missing retry", 429, `{"code":"RATE_LIMITED","message":"limited"}`, "", false},
			} {
				t.Run(c.name, func(t *testing.T) {
					rec := httptest.NewRecorder()
					rec.Header().Set("Content-Type", "application/json")
					if c.retry != "" {
						rec.Header().Set("Retry-After", c.retry)
					}
					rec.WriteHeader(c.status)
					_, _ = rec.WriteString(c.body)
					err := openAPIValidateResponse(op, rec)
					if (err == nil) != c.valid {
						t.Fatalf("valid=%v err=%v", c.valid, err)
					}
				})
			}
		})
	}
}
