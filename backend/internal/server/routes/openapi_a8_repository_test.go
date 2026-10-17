package routes_test

import (
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"net/http/httptest"
	"testing"
)

// SQLmock drives the real Ent delete and real HTTP path. It does not execute
// PostgreSQL constraints, cascading/FK effects or a live database transaction.
func TestOpenAPIA8DeleteSQLContract(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, root := range []string{"channel-monitors", "channel-monitor-templates"} {
		t.Run(root, func(t *testing.T) {
			db, mock, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { _ = db.Close() })
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			f := openAPIA8NewFixture(t)
			table := "channel_monitors"
			if root == "channel-monitors" {
				f.monitorRepo = repository.NewChannelMonitorRepository(client, db)
			} else {
				f.templateRepo = repository.NewChannelMonitorRequestTemplateRepository(client, db)
				table = "channel_monitor_request_templates"
			}
			r := f.router(t)
			for _, n := range []int64{0, 1, 0} {
				t.Run(fmt.Sprint(n), func(t *testing.T) {
					mock.ExpectExec(`^DELETE FROM "` + table + `" WHERE "` + table + `"\."id" = \$1$`).WithArgs(int64(301)).WillReturnResult(sqlmock.NewResult(0, n))
					req := httptest.NewRequest("DELETE", "/api/v1/admin/"+root+"/301", nil)
					req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
					rec := httptest.NewRecorder()
					r.ServeHTTP(rec, req)
					want := 200
					if n == 0 {
						want = 404
					}
					if rec.Code != want {
						t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
					}
					if e := openAPIValidateResponse(doc.Paths["/api/v1/admin/"+root+"/{id}"]["delete"], rec); e != nil {
						t.Fatal(e)
					}
					if e := mock.ExpectationsWereMet(); e != nil {
						t.Fatal(e)
					}
				})
			}
		})
	}
}
