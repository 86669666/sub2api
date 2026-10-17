package routes_test

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

func openAPIA8Call(t *testing.T, r *gin.Engine, method, path, id, body, query, key string, want int, invalid bool) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	doc, _ := openAPILoad(t)
	op := doc.Paths[path][strings.ToLower(method)]
	req := httptest.NewRequest(method, openAPIA2URL(path, id)+query, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+openAPIA1Token(t, nil, openAPIA1SigningKey))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	params := map[string]string{}
	if strings.Contains(path, "{id}") {
		params["id"] = id
	}
	if e := openAPIValidateRequest(op, req, params, []byte(body)); (e != nil) != invalid {
		t.Fatalf("request invalid=%v error=%v", invalid, e)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != want {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
	}
	if e := openAPIValidateResponse(op, rec); e != nil {
		t.Fatalf("response contract: %v body=%s", e, rec.Body.String())
	}
	var out map[string]any
	if e := openAPIDecode(rec.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(rec.Body.String(), openAPIA8Secret) || strings.Contains(rec.Body.String(), "cipher:") {
		t.Fatal("key leaked")
	}
	return rec, out
}
func TestOpenAPIA8RequestSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, root, method, body string
		status                   int
		invalid                  bool
	}{
		{"monitor required", "channel-monitors", "POST", `{}`, 400, true},
		{"template required", "channel-monitor-templates", "POST", `{}`, 400, true},
		{"monitor null required", "channel-monitors", "POST", `{"name":null,"provider":"openai","interval_seconds":60}`, 400, true},
		{"template blank", "channel-monitor-templates", "PUT", `{"name":"  "}`, 400, false},
		{"monitor blank update accepted", "channel-monitors", "PUT", `{"name":"  "}`, 200, false},
		{"unknown ignored", "channel-monitors", "PUT", `{"created_by":123,"api_key_masked":"spoof","unknown":{"x":1}}`, 200, false},
		{"null retains", "channel-monitors", "PUT", `null`, 200, false},
		{"template null retains", "channel-monitor-templates", "PUT", `{"name":null,"extra_headers":null,"body_override":null}`, 200, false},
		{"fraction integer", "channel-monitors", "PUT", `{"interval_seconds":60.0}`, 400, true},
		{"exponent integer", "channel-monitors", "PUT", `{"interval_seconds":6e1}`, 400, true},
		{"integer overflow", "channel-monitors", "PUT", `{"interval_seconds":9223372036854775808}`, 400, true},
		{"interval bounds", "channel-monitors", "PUT", `{"interval_seconds":14}`, 400, false},
		{"jitter coupled", "channel-monitors", "PUT", `{"interval_seconds":60,"jitter_seconds":46}`, 400, false},
		{"blocked provider", "channel-monitors", "PUT", `{"provider":"opencode_go"}`, 400, false},
		{"responses requires openai", "channel-monitors", "PUT", `{"provider":"anthropic","api_mode":"responses"}`, 400, false},
		{"quota requires account", "channel-monitors", "PUT", `{"check_mode":"quota","account_id":0}`, 400, false},
		{"http blocked", "channel-monitors", "PUT", `{"endpoint":"http://8.8.8.8"}`, 400, false},
		{"loopback blocked", "channel-monitors", "PUT", `{"endpoint":"https://127.0.0.1"}`, 400, false},
		{"query blocked", "channel-monitors", "PUT", `{"endpoint":"https://8.8.8.8?x=y"}`, 400, false},
		{"header type", "channel-monitors", "PUT", `{"extra_headers":{"X-Test":23}}`, 400, true},
		{"header forbidden", "channel-monitors", "PUT", `{"extra_headers":{"hOsT":"synthetic"}}`, 400, false},
		{"body must object", "channel-monitors", "PUT", `{"body_override":[]}`, 400, true},
		{"merge empty", "channel-monitors", "PUT", `{"body_override_mode":"merge","body_override":{}}`, 400, false},
		{"replace chat missing messages", "channel-monitors", "PUT", `{"body_override_mode":"replace","body_override":{"temperature":1}}`, 400, false},
		{"replace responses missing instructions", "channel-monitors", "PUT", `{"api_mode":"responses","body_override_mode":"replace","body_override":{"input":"x"}}`, 400, false},
		{"arbitrary JSON", "channel-monitors", "PUT", `{"body_override_mode":"merge","body_override":{"messages":[{"role":"user","content":"synthetic"}],"nested":[null,true,1.25,{"unknown":[1]}]}}`, 200, false},
		{"template immutable provider", "channel-monitor-templates", "PUT", `{"provider":"grok","description":"  updated  "}`, 200, false},
		{"template merge empty", "channel-monitor-templates", "PUT", `{"body_override_mode":"merge","body_override":{}}`, 400, false},
		{"unicode bound", "channel-monitor-templates", "PUT", `{"name":"` + strings.Repeat("文", 101) + `"}`, 400, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := openAPIA8NewFixture(t)
			r := f.router(t)
			path := "/api/v1/admin/" + tc.root
			id := "301"
			if tc.root == "channel-monitor-templates" {
				id = "501"
			}
			if tc.method == "PUT" {
				path += "/{id}"
			}
			_, b := openAPIA8Call(t, r, tc.method, path, id, tc.body, "", "", tc.status, tc.invalid)
			if tc.status >= 400 && f.monitors.writes+f.templates.writes != 0 {
				t.Fatal("invalid request mutated repository")
			}
			if tc.status == 200 {
				if f.monitors.writes+f.templates.writes != 1 {
					t.Fatal("expected one update")
				}
				if tc.root == "channel-monitors" && openAPIA5Map(t, b["data"])["created_by"] != json.Number("99") {
					t.Fatal("update changed creator")
				}
				if tc.name == "template immutable provider" && f.templates.rows[501].Provider != "openai" {
					t.Fatal("provider changed")
				}
			}
		})
	}
}
func TestOpenAPIA8LifecycleProjection(t *testing.T) {
	f := openAPIA8NewFixture(t)
	r := f.router(t)
	root := "/api/v1/admin/channel-monitors"
	body := `{"name":"  New  ","provider":"openai","endpoint":"https://8.8.8.8/base/","api_key":" ` + openAPIA8Secret + ` ","primary_model":" model ","extra_models":[" x ","x","","y"],"group_name":" group ","interval_seconds":60,"template_id":501,"body_override":{"arbitrary":[true,null,2.5]},"created_by":123}`
	_, b := openAPIA8Call(t, r, "POST", root, "", body, "", "", 201, false)
	data := openAPIA5Map(t, b["data"])
	m := f.monitors.rows[901]
	if m.Name != "New" || m.APIKey != "cipher:"+openAPIA8Secret || m.Endpoint != "https://8.8.8.8/base" || m.PrimaryModel != "model" || m.GroupName != "group" || !m.Enabled || len(m.ExtraModels) != 2 || m.CreatedBy != 41 || data["api_key_masked"] != "A8SX***" {
		t.Fatal("create normalization/projection drift")
	}
	_, _ = openAPIA8Call(t, r, "PUT", root+"/{id}", "901", `{"api_key":"","extra_models":[],"extra_headers":{},"body_override":{},"clear_template":true,"template_id":501,"account_id":0}`, "", "", 200, false)
	m = f.monitors.rows[901]
	if m.APIKey != "cipher:"+openAPIA8Secret || m.TemplateID != nil || m.AccountID != nil || len(m.ExtraModels) != 0 || len(m.ExtraHeaders) != 0 || len(m.BodyOverride) != 0 {
		t.Fatal("clear/retain drift")
	}
	f.encryptor.decryptFail = true
	_, b = openAPIA8Call(t, r, "GET", root+"/{id}", "901", "", "", "", 200, false)
	if openAPIA5Map(t, b["data"])["api_key_decrypt_failed"] != true {
		t.Fatal("decrypt flag drift")
	}
}
func TestOpenAPIA8QuerySemantics(t *testing.T) {
	for _, tc := range []struct {
		query      string
		page, size int
		enabled    *bool
	}{
		{"?page=bad&page_size=-1&enabled=unknown", 1, 20, nil},
		{"?page=2&page=9&page_size=999&enabled=%20YeS%20", 2, 100, func() *bool { v := true; return &v }()},
		{"?page_size=&limit=7&enabled=no", 1, 7, func() *bool { v := false; return &v }()},
	} {
		t.Run(tc.query, func(t *testing.T) {
			f := openAPIA8NewFixture(t)
			r := f.router(t)
			openAPIA8Call(t, r, "GET", "/api/v1/admin/channel-monitors", "", "", tc.query, "", 200, false)
			p := f.monitors.params
			if p.Page != tc.page || p.PageSize != tc.size || (p.Enabled == nil) != (tc.enabled == nil) || (p.Enabled != nil && *p.Enabled != *tc.enabled) {
				t.Fatalf("query drift: %+v", p)
			}
		})
	}
	for _, tc := range []struct {
		q     string
		limit int
		model string
	}{{"?limit=0", 100, ""}, {"?limit=9999&model=%20m%20", 1000, "m"}, {"?limit=%205%20", 100, ""}, {"?limit=5&limit=8", 5, ""}} {
		t.Run(tc.q, func(t *testing.T) {
			f := openAPIA8NewFixture(t)
			r := f.router(t)
			openAPIA8Call(t, r, "GET", "/api/v1/admin/channel-monitors/{id}/history", "301", "", tc.q, "", 200, false)
			if f.monitors.historyLimit != tc.limit || f.monitors.historyModel != tc.model {
				t.Fatal("history query drift")
			}
		})
	}
}
func TestOpenAPIA8TemplateApplySelection(t *testing.T) {
	f := openAPIA8NewFixture(t)
	original := f.monitors.rows[301]
	original.DuplicateOperationID = "synthetic-operation"
	f.monitors.rows[301] = original
	for _, id := range []int64{302, 303, 304} {
		m := openAPIA8Clone(original)
		m.ID = id
		if id == 302 {
			m.Provider = "grok"
		}
		if id == 303 {
			m.APIMode = "responses"
		}
		if id == 304 {
			m.TemplateID = nil
		}
		f.monitors.rows[id] = m
	}
	r := f.router(t)
	root := "/api/v1/admin/channel-monitor-templates/{id}"
	_, b := openAPIA8Call(t, r, "GET", root+"/monitors", "501", "", "", "", 200, false)
	if len(openAPIA5Array(t, openAPIA5Map(t, b["data"])["items"])) != 3 {
		t.Fatal("association projection drift")
	}
	_, b = openAPIA8Call(t, r, "POST", root+"/apply", "501", `{"monitor_ids":[301,301,302,303,304,0,-1,null,9999]}`, "", "", 200, false)
	if openAPIA5Map(t, b["data"])["affected"] != json.Number("1") || f.monitors.rows[301].ExtraHeaders["X-Template"] != "yes" || f.monitors.rows[301].DuplicateOperationID != "synthetic-operation" || f.templates.writes != 0 {
		t.Fatal("apply selection/metadata drift")
	}
	for _, id := range []int64{302, 303, 304} {
		if f.monitors.rows[id].ExtraHeaders["X-Test"] != "synthetic" {
			t.Fatal("unmatched row changed")
		}
	}
	openAPIA8Call(t, r, "POST", root+"/apply", "501", `{"monitor_ids":[]}`, "", "", 400, false)
	openAPIA8Call(t, r, "POST", root+"/apply", "501", `{}`, "", "", 400, true)
	openAPIA8Call(t, r, "POST", root+"/apply", "501", `{"monitor_ids":[301.0]}`, "", "", 400, true)
	_, b = openAPIA8Call(t, r, "POST", root+"/apply", "501", `{"monitor_ids":[9999]}`, "", "", 200, false)
	if openAPIA5Map(t, b["data"])["affected"] != json.Number("0") {
		t.Fatal("unmatched affected drift")
	}
}
func TestOpenAPIA8DuplicateReplayRecovery(t *testing.T) {
	for _, mode := range []string{"replay", "recovery-store", "recovery-progress"} {
		t.Run(mode, func(t *testing.T) {
			f := openAPIA8NewFixture(t)
			r := f.router(t)
			path := "/api/v1/admin/channel-monitors/{id}/duplicate"
			_, first := openAPIA8Call(t, r, "POST", path, "301", `ignored body`, "", "synthetic-dup", 200, false)
			copied := f.monitors.rows[901]
			if copied.Enabled || copied.CreatedBy != 41 || copied.APIKey != "cipher:"+openAPIA8Secret || copied.LastCheckedAt != nil || copied.DuplicateOperationID == "" {
				t.Fatal("duplicate projection drift")
			}
			copied.ExtraHeaders["X-Test"] = "changed"
			if f.monitors.rows[301].ExtraHeaders["X-Test"] != "synthetic" {
				t.Fatal("copy aliases source headers")
			}
			if mode == "recovery-store" {
				f.idem.fail = true
			}
			if mode == "recovery-progress" {
				f.idem.record.Status = "processing"
			}
			delete(f.monitors.rows, 301)
			rec, second := openAPIA8Call(t, r, "POST", path, "301", `different ignored body`, "", "synthetic-dup", 200, false)
			header := "X-Idempotency-Replayed"
			if mode != "replay" {
				header = "X-Idempotency-Recovered"
			}
			if rec.Header().Get(header) != "true" || f.monitors.writes != 1 || openAPIA5Map(t, first["data"])["id"] != openAPIA5Map(t, second["data"])["id"] {
				t.Fatal("replay/recovery drift")
			}
		})
	}
}
func TestOpenAPIA8QuotaPersistenceFailure(t *testing.T) {
	for _, op := range []string{"history-write", "mark"} {
		t.Run(op, func(t *testing.T) {
			f := openAPIA8NewFixture(t)
			m := f.monitors.rows[301]
			m.CheckMode = "quota"
			m.Enabled = false
			m.AccountID = nil
			f.monitors.rows[301] = m
			f.monitors.failOp = op
			r := f.router(t)
			_, b := openAPIA8Call(t, r, "POST", "/api/v1/admin/channel-monitors/{id}/run", "301", "", "", "", 200, false)
			row := openAPIA5Map(t, openAPIA5Array(t, openAPIA5Map(t, b["data"])["results"])[0])
			if openAPIA5Map(t, row["quota"])["success"] != false {
				t.Fatal("missing account did not fail closed")
			}
		})
	}
}

// Synthetic codec evidence only: no active probe, scheduler or provider is run.
func TestOpenAPIA8SyntheticProbeResponseCodec(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/api/v1/admin/channel-monitors/{id}/run"]["post"]
	for _, status := range []string{"operational", "degraded", "failed", "error"} {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "application/json")
		rec.WriteHeader(200)
		rec.Body.WriteString(`{"code":0,"message":"success","data":{"results":[{"model":"synthetic","status":"` + status + `","latency_ms":25,"ping_latency_ms":null,"message":"synthetic codec only","checked_at":"2035-01-02T03:04:05Z"}]}}`)
		if e := openAPIValidateResponse(op, rec); e != nil {
			t.Fatal(e)
		}
	}
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(200)
	rec.Body.WriteString(`{"code":0,"message":"success","data":{"results":[{"model":"synthetic","status":"unknown","latency_ms":null,"ping_latency_ms":null,"message":"synthetic","checked_at":"2035-01-02T03:04:05Z"}]}}`)
	if openAPIValidateResponse(op, rec) == nil {
		t.Fatal("invalid probe status accepted")
	}
}
