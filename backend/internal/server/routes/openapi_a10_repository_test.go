package routes_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime" // Register the real Ent defaults and validators used by write builders.
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const openAPIA10ListSQL = `^SELECT .* FROM "error_passthrough_rules" ORDER BY "error_passthrough_rules"\."priority" ASC$`
const openAPIA10GetSQL = `^SELECT .* FROM "error_passthrough_rules" WHERE "error_passthrough_rules"\."id" = \$1.*$`

var openAPIA10Columns = []string{"id", "name", "enabled", "priority", "error_codes", "keywords", "match_mode", "platforms", "passthrough_code", "response_code", "passthrough_body", "custom_message", "skip_monitoring", "description", "created_at", "updated_at"}

func openAPIA10Rows(populated bool) *sqlmock.Rows {
	rows := sqlmock.NewRows(openAPIA10Columns)
	if populated {
		now := time.Date(2035, 1, 2, 3, 4, 5, 0, time.UTC)
		rows.AddRow(301, "Rule", true, -2, []byte("[429,503]"), nil, "all", nil, true, 429, true, "literal message", true, "description", now, now)
	}
	return rows
}
func openAPIA10SQL(t *testing.T) (service.ErrorPassthroughRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	return repository.NewErrorPassthroughRepository(client), m
}
func openAPIA10SQLDone(t *testing.T, m sqlmock.Sqlmock) {
	t.Helper()
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

// SQLmock executes the real Ent builder/repository and HTTP chain, never a live DB.
// It proves generated SQL and response/error mapping, not PostgreSQL constraints.
func TestOpenAPIA10SQLReadsAndDelete(t *testing.T) {
	d, _ := openAPILoad(t)
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprintf("list populated=%t", populated), func(t *testing.T) {
			repo, m := openAPIA10SQL(t)
			m.ExpectQuery(openAPIA10ListSQL).WillReturnRows(openAPIA10Rows(populated))
			_, b := openAPIA10Run(t, "get", openAPIA10Root, d.Paths[openAPIA10Root]["get"], openAPIA10Case{status: 200, change: func(f *openAPIA10Fixture) { f.repoOverride = repo }})
			rows := openAPIA5Array(t, b["data"])
			if (len(rows) == 1) != populated {
				t.Fatal("list shape")
			}
			if populated {
				row := openAPIA5Map(t, rows[0])
				if len(openAPIA5Array(t, row["keywords"])) != 0 || len(openAPIA5Array(t, row["platforms"])) != 0 {
					t.Fatal("nullable SQL arrays not normalized")
				}
			}
			openAPIA10SQLDone(t, m)
		})
	}
	path := openAPIA10Root + "/{id}"
	for _, method := range []string{"get", "put"} {
		t.Run(method+" initial nil", func(t *testing.T) {
			repo, m := openAPIA10SQL(t)
			m.ExpectQuery(openAPIA10GetSQL).WithArgs(int64(301)).WillReturnRows(openAPIA10Rows(false))
			openAPIA10Run(t, method, path, d.Paths[path][method], openAPIA10Case{status: 404, change: func(f *openAPIA10Fixture) { f.repoOverride = repo }})
			openAPIA10SQLDone(t, m)
		})
	}
	for _, affected := range []int64{0, 1} {
		t.Run(fmt.Sprintf("delete affected=%d", affected), func(t *testing.T) {
			repo, m := openAPIA10SQL(t)
			m.ExpectExec(`^DELETE FROM "error_passthrough_rules" WHERE "error_passthrough_rules"\."id" = \$1$`).WithArgs(int64(301)).WillReturnResult(sqlmock.NewResult(0, affected))
			want := 500
			if affected == 1 {
				want = 200
				m.ExpectQuery(openAPIA10ListSQL).WillReturnError(fmt.Errorf("PRIVATE-A10-RELOAD"))
			}
			openAPIA10Run(t, "delete", path, d.Paths[path]["delete"], openAPIA10Case{status: want, change: func(f *openAPIA10Fixture) { f.repoOverride = repo }})
			openAPIA10SQLDone(t, m)
		})
	}
}
func TestOpenAPIA10EntNameAndCreate(t *testing.T) {
	d, _ := openAPILoad(t)
	for _, tc := range []struct {
		name  string
		valid bool
	}{{strings.Repeat("x", 100), true}, {strings.Repeat("x", 101), false}, {strings.Repeat("界", 33), true}, {strings.Repeat("界", 34), false}} {
		t.Run(fmt.Sprintf("name bytes=%d", len(tc.name)), func(t *testing.T) {
			repo, m := openAPIA10SQL(t)
			want := 500
			if tc.valid {
				want = 200
				m.ExpectQuery(`^INSERT INTO "error_passthrough_rules" .* RETURNING "id"$`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(901))
				m.ExpectQuery(openAPIA10ListSQL).WillReturnError(fmt.Errorf("PRIVATE-A10-RELOAD"))
			}
			openAPIA10Run(t, "post", openAPIA10Root, d.Paths[openAPIA10Root]["post"], openAPIA10Case{status: want, body: `{"name":"` + tc.name + `","error_codes":[429]}`, change: func(f *openAPIA10Fixture) { f.repoOverride = repo }})
			openAPIA10SQLDone(t, m)
		})
	}
}
func TestOpenAPIA10EntPartialUpdate(t *testing.T) {
	d, _ := openAPILoad(t)
	path := openAPIA10Root + "/{id}"
	for _, kind := range []string{"commit with cleared arrays and retained pointers", "missing during write", "reread storage failure"} {
		t.Run(kind, func(t *testing.T) {
			repo, m := openAPIA10SQL(t)
			m.ExpectQuery(openAPIA10GetSQL).WithArgs(int64(301)).WillReturnRows(openAPIA10Rows(true))
			m.ExpectBegin()
			// The request sends null pointer fields: handler retains values and Ent writes
			// those values; empty arrays become SQL NULL and normalize back to [].
			update := `^UPDATE "error_passthrough_rules" SET "keywords" = NULL, "platforms" = NULL, "updated_at" = \$1, "name" = \$2, "enabled" = \$3, "priority" = \$4, "error_codes" = \$5, "match_mode" = \$6, "passthrough_code" = \$7, "response_code" = \$8, "passthrough_body" = \$9, "custom_message" = \$10, "skip_monitoring" = \$11, "description" = \$12 WHERE "id" = \$13$`
			affected := int64(1)
			if kind == "missing during write" {
				affected = 0
			}
			m.ExpectExec(update).WithArgs(sqlmock.AnyArg(), "Rule", true, -2, []byte("[429,503]"), "all", true, 429, true, "literal message", true, "description", int64(301)).WillReturnResult(sqlmock.NewResult(0, affected))
			query := m.ExpectQuery(`^SELECT .* FROM "error_passthrough_rules" WHERE "id" = \$1$`).WithArgs(int64(301))
			want := 500
			switch kind {
			case "missing during write":
				query.WillReturnRows(openAPIA10Rows(false))
				m.ExpectRollback()
			case "reread storage failure":
				query.WillReturnError(fmt.Errorf("PRIVATE-A10-REREAD"))
				m.ExpectRollback()
			default:
				want = 200
				query.WillReturnRows(openAPIA10Rows(true))
				m.ExpectCommit()
				m.ExpectQuery(openAPIA10ListSQL).WillReturnRows(openAPIA10Rows(true))
			}
			openAPIA10Run(t, "put", path, d.Paths[path]["put"], openAPIA10Case{status: want, body: `{"keywords":[],"platforms":[],"response_code":null,"custom_message":null,"description":null}`, change: func(f *openAPIA10Fixture) { f.repoOverride = repo }})
			openAPIA10SQLDone(t, m)
		})
	}
}
