package routes_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// SQLmock drives the actual repository through SetupRouter. No PostgreSQL, Redis
// socket, provider or production configuration is used. SQL execution itself is not proved.
func TestOpenAPIA7ConfigRepository(t *testing.T) {
	const path = "/api/v1/admin/channel-monitor-v2/config"
	const selectSQL = `SELECT version, enabled, refresh_interval_seconds, platforms, group_ids, COALESCE(ignored_error_categories, '{}'), COALESCE(health_thresholds, '{}'::jsonb), updated_at, updated_by FROM channel_monitor_v2_config WHERE id = 1`
	const updateSQL = `UPDATE channel_monitor_v2_config SET version = version + 1, enabled = $1, refresh_interval_seconds = $2, platforms = $3, group_ids = $4, ignored_error_categories = $5, health_thresholds = $6, updated_by = $7, updated_at = NOW() WHERE id = 1 AND version = $8 RETURNING version, enabled, refresh_interval_seconds, platforms, group_ids, COALESCE(ignored_error_categories, '{}'), COALESCE(health_thresholds, '{}'::jsonb), updated_at, updated_by`
	d, _ := openAPILoad(t)
	stamp := time.Date(2036, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, method := range []string{"get", "put"} {
		for _, kind := range []string{"defaults", "null platforms", "partial thresholds", "bad thresholds tolerated", "missing", "query failure", "bad platforms", "bad scan", "reject before SQL"} {
			t.Run(method+"/"+kind, func(t *testing.T) {
				db, mock, err := sqlmock.New()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Close() })
				f := openAPIA7NewFixture(t)
				f.repo = repository.NewChannelMonitorV2Repository(db)
				body := ""
				if method == "put" {
					body = `{"version":7,"enabled":true,"refresh_interval_seconds":0,"group_ids":[99,11,99],"platforms":[{"platform":" OpenAI ","enabled":true,"models":[" z ","a","a"]}],"ignored_error_categories":["TIMEOUT","garbage"],"updated_by":999,"updated_at":"2001-01-01T00:00:00Z"}`
				}
				status := 200
				if kind == "reject before SQL" {
					status = 403
					f.a1.settings.values[service.SettingKeyChannelMonitorEnabled] = "false"
				}
				if kind != "reject before SQL" {
					var q *sqlmock.ExpectedQuery
					if method == "put" {
						thresholds, e := json.Marshal(service.DefaultChannelMonitorV2HealthThresholds())
						if e != nil {
							t.Fatal(e)
						}
						q = mock.ExpectQuery(regexp.QuoteMeta(updateSQL)).WithArgs(true, 300, []byte(`[{"platform":"openai","enabled":true,"models":["a","z"]}]`), "{11,99}", `{"timeout"}`, thresholds, int64(41), 7)
					} else {
						q = mock.ExpectQuery(regexp.QuoteMeta(selectSQL))
					}
					platforms := `[{"platform":"openai","enabled":true,"models":["a","z"]}]`
					thresholds := `{}`
					switch kind {
					case "null platforms":
						platforms = `null`
					case "partial thresholds":
						thresholds = `{"minimum_sample":10001,"warning_error_rate":0.3}`
					case "bad thresholds tolerated":
						thresholds = `{`
					case "bad platforms":
						platforms = `{`
					}
					switch kind {
					case "missing":
						q.WillReturnError(sql.ErrNoRows)
						status = 500
						if method == "put" {
							status = 409
						}
					case "query failure":
						q.WillReturnError(fmt.Errorf("PRIVATE-A7-SQL"))
						status = 500
					case "bad scan":
						q.WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("not-int"))
						status = 500
					default:
						q.WillReturnRows(sqlmock.NewRows([]string{"version", "enabled", "refresh_interval_seconds", "platforms", "group_ids", "ignored_error_categories", "health_thresholds", "updated_at", "updated_by"}).AddRow(8, true, 300, []byte(platforms), "{11,99}", nil, []byte(thresholds), stamp, 41))
						if kind == "bad platforms" {
							status = 500
						}
					}
				}
				r := f.router(t)
				req := httptest.NewRequest(strings.ToUpper(method), path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				f.auditService.Stop()
				if rec.Code != status {
					t.Fatalf("status=%d want=%d body=%s", rec.Code, status, rec.Body.String())
				}
				if err := openAPIValidateResponse(d.Paths[path][method], rec); err != nil {
					t.Fatal(err)
				}
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(rec.Body.String(), "PRIVATE-A7") {
					t.Fatal("SQL error leaked")
				}
				if status == 200 {
					var b map[string]any
					if err := openAPIDecode(rec.Body.Bytes(), &b); err != nil {
						t.Fatal(err)
					}
					v := openAPIA5Map(t, b["data"])
					if v["version"] != json.Number("8") || v["updated_at"] != stamp.Format(time.RFC3339) || v["updated_by"] != json.Number("41") || len(openAPIA5Array(t, v["ignored_error_categories"])) != 0 {
						t.Fatal("SQL metadata/defaults drift")
					}
					h := openAPIA5Map(t, v["health_thresholds"])
					minimum := "50"
					if kind == "partial thresholds" {
						minimum = "10000"
					}
					if h["minimum_sample"] != json.Number(minimum) {
						t.Fatal("SQL threshold normalization drift")
					}
				}
			})
		}
	}
}
