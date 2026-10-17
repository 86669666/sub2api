package routes_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func openAPIA10Expect(fields string) func(*testing.T, *openAPIA10Fixture, map[string]any) {
	return func(t *testing.T, _ *openAPIA10Fixture, b map[string]any) {
		t.Helper()
		var want map[string]any
		if e := openAPIDecode([]byte(fields), &want); e != nil {
			t.Fatal(e)
		}
		got := openAPIA5Map(t, b["data"])
		for k, v := range want {
			if !reflect.DeepEqual(got[k], v) {
				t.Fatalf("%s=%v want=%v", k, got[k], v)
			}
		}
	}
}
func TestOpenAPIA10DefaultsAndPartialUpdates(t *testing.T) {
	d, _ := openAPILoad(t)
	defaults := `{"enabled":true,"priority":0,"error_codes":[429],"keywords":[],"match_mode":"any","platforms":[],"passthrough_code":true,"response_code":null,"passthrough_body":true,"custom_message":null,"skip_monitoring":false,"description":null}`
	allNull := `{"name":null,"enabled":null,"priority":null,"error_codes":null,"keywords":null,"match_mode":null,"platforms":null,"passthrough_code":null,"response_code":null,"passthrough_body":null,"custom_message":null,"skip_monitoring":null,"description":null}`
	keep := `{"name":"Rule","enabled":true,"priority":-2,"error_codes":[429,503],"keywords":["quota","Busy"],"match_mode":"all","platforms":["OpenAI","custom"],"passthrough_code":true,"response_code":429,"passthrough_body":true,"custom_message":"literal message","skip_monitoring":true,"description":"description"}`
	for _, method := range []string{"post", "put"} {
		path := openAPIA10Root
		if method == "put" {
			path += "/{id}"
		}
		cs := []openAPIA10Case{}
		if method == "post" {
			cs = append(cs, openAPIA10Case{name: "defaults", status: 200, inspect: openAPIA10Expect(defaults)},
				openAPIA10Case{name: "null defaults", status: 200, body: `{"name":"Created","error_codes":[429],"enabled":null,"priority":null,"keywords":null,"match_mode":null,"platforms":null,"passthrough_code":null,"response_code":null,"passthrough_body":null,"custom_message":null,"skip_monitoring":null,"description":null}`, inspect: openAPIA10Expect(defaults)},
				openAPIA10Case{name: "empty mode default", status: 200, body: `{"name":"x","error_codes":[429],"match_mode":""}`, inspect: openAPIA10Expect(`{"match_mode":"any"}`)},
				openAPIA10Case{name: "null root", status: 400, invalid: true, body: "null"},
				openAPIA10Case{name: "null name", status: 400, invalid: true, body: `{"name":null,"error_codes":[429]}`},
				openAPIA10Case{name: "missing name", status: 400, invalid: true, body: `{"error_codes":[429]}`},
				openAPIA10Case{name: "conditional null code", status: 400, body: `{"name":"x","error_codes":[429],"passthrough_code":false,"response_code":null}`},
				openAPIA10Case{name: "conditional null message", status: 400, body: `{"name":"x","error_codes":[429],"passthrough_body":false,"custom_message":null}`})
		} else {
			for _, body := range []string{"{}", "null", allNull} {
				cs = append(cs, openAPIA10Case{name: "preserve " + body, status: 200, body: body, inspect: openAPIA10Expect(keep)})
			}
			cs = append(cs,
				openAPIA10Case{name: "zero false empty replace", status: 200, body: `{"name":" ","enabled":false,"priority":0,"error_codes":[],"platforms":[],"response_code":0,"custom_message":"","description":"","skip_monitoring":false,"match_mode":"any"}`, inspect: openAPIA10Expect(`{"name":" ","enabled":false,"priority":0,"error_codes":[],"platforms":[],"response_code":0,"custom_message":"","description":"","skip_monitoring":false,"match_mode":"any","keywords":["quota","Busy"]}`)},
				openAPIA10Case{name: "keywords empty with codes", status: 200, body: `{"keywords":[]}`, inspect: openAPIA10Expect(`{"keywords":[],"error_codes":[429,503]}`)},
				openAPIA10Case{name: "null retains conditional values", status: 200, body: `{"passthrough_code":false,"response_code":null,"passthrough_body":false,"custom_message":null}`, inspect: openAPIA10Expect(`{"passthrough_code":false,"response_code":429,"passthrough_body":false,"custom_message":"literal message"}`)},
				openAPIA10Case{name: "empty mode invalid", status: 400, body: `{"match_mode":""}`})
		}
		cs = append(cs,
			openAPIA10Case{name: "literal duplicate unrestricted values", status: 200, body: `{"name":"  Literal  ","priority":-9223372036854775808,"error_codes":[-1,0,1000,1000],"keywords":[""," Busy "," Busy "],"platforms":["UNKNOWN"," openai ","UNKNOWN"],"match_mode":"all","passthrough_code":false,"response_code":1000,"passthrough_body":false,"custom_message":" ","description":"  ","skip_monitoring":false,"ignored":{"nested":[1,true]}}`, inspect: openAPIA10Expect(`{"name":"  Literal  ","priority":-9223372036854775808,"error_codes":[-1,0,1000,1000],"keywords":[""," Busy "," Busy "],"platforms":["UNKNOWN"," openai ","UNKNOWN"],"response_code":1000,"custom_message":" ","description":"  "}`)},
			openAPIA10Case{name: "null array elements bind zero", status: 200, body: `{"name":"x","priority":9223372036854775807,"error_codes":[null],"keywords":[null],"platforms":[null]}`, inspect: openAPIA10Expect(`{"priority":9223372036854775807,"error_codes":[0],"keywords":[""],"platforms":[""]}`)},
			openAPIA10Case{name: "inactive conditional fields unrestricted", status: 200, body: `{"name":"x","error_codes":[1],"passthrough_code":true,"response_code":-1,"passthrough_body":true,"custom_message":""}`, inspect: openAPIA10Expect(`{"response_code":-1,"custom_message":""}`)},
			openAPIA10Case{name: "negative conditional response", status: 400, body: `{"name":"x","error_codes":[1],"passthrough_code":false,"response_code":-1}`})
		for _, tc := range cs {
			t.Run(method+"/"+tc.name, func(t *testing.T) { openAPIA10Run(t, method, path, d.Paths[path][method], tc) })
		}
	}
}
func TestOpenAPIA10TypedBinding(t *testing.T) {
	d, _ := openAPILoad(t)
	bad := map[string]string{"bool": "1", "int": `"1"`, "float": "1.0", "exponent": "1e0", "overflow": "9223372036854775808", "array": `["bad"]`, "string": "false"}
	for _, method := range []string{"post", "put"} {
		path := openAPIA10Root
		if method == "put" {
			path += "/{id}"
		}
		for _, tc := range []struct{ field, kind string }{{"enabled", "bool"}, {"skip_monitoring", "bool"}, {"priority", "int"}, {"priority", "float"}, {"response_code", "exponent"}, {"priority", "overflow"}, {"error_codes", "array"}, {"name", "string"}, {"keywords", "bool"}, {"platforms", "bool"}, {"custom_message", "bool"}, {"description", "bool"}} {
			body := fmt.Sprintf(`{"name":"x","error_codes":[1],"%s":%s}`, tc.field, bad[tc.kind])
			t.Run(method+"/"+tc.field+"/"+tc.kind, func(t *testing.T) {
				openAPIA10Run(t, method, path, d.Paths[path][method], openAPIA10Case{body: body, status: 400, invalid: true})
			})
		}
	}
}
func TestOpenAPIA10ResponseShapeAndCoverage(t *testing.T) {
	d, inv := openAPILoad(t)
	for _, key := range openAPIA10Routes {
		parts := strings.SplitN(key, " ", 2)
		m := strings.ToLower(parts[0])
		p := parts[1]
		op := d.Paths[p][m]
		if op.OperationID == "" {
			t.Fatal("missing A10 operation")
		}
		found := false
		for _, r := range inv.Normal {
			if r.Method == parts[0] && r.Path == strings.ReplaceAll(p, "{id}", ":id") {
				found = r.Coverage == "covered"
			}
		}
		if !found {
			t.Fatalf("coverage demoted: %s", key)
		}
	}
	_, b := openAPIA10Run(t, "get", openAPIA10Root+"/{id}", d.Paths[openAPIA10Root+"/{id}"]["get"], openAPIA10Case{status: 200})
	schema := d.Paths[openAPIA10Root+"/{id}"]["get"].Responses["200"].Content["application/json"].Schema
	if e := openAPIValidateValue(schema, openAPIA10ResponseClone(t, b)); e != nil {
		t.Fatal("positive clone rejected", e)
	}
	data := openAPIA5Map(t, b["data"])
	for k := range data {
		t.Run("required "+k, func(t *testing.T) {
			v := openAPIA10ResponseClone(t, b)
			delete(openAPIA5Map(t, v["data"]), k)
			if openAPIValidateValue(schema, v) == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
	for _, kind := range []string{"unknown", "nullable type", "array null", "array element", "timestamp", "integer"} {
		t.Run(kind, func(t *testing.T) {
			v := openAPIA10ResponseClone(t, b)
			x := openAPIA5Map(t, v["data"])
			switch kind {
			case "unknown":
				x["credential"] = "synthetic"
			case "nullable type":
				x["response_code"] = "429"
			case "array null":
				x["keywords"] = nil
			case "array element":
				x["error_codes"] = []any{"429"}
			case "timestamp":
				x["created_at"] = "yesterday"
			case "integer":
				x["priority"] = json.Number("0.5")
			}
			if openAPIValidateValue(schema, v) == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}
func TestOpenAPIA10CacheConsumerAndAudit(t *testing.T) {
	d, _ := openAPILoad(t)
	f, _ := openAPIA10Run(t, "post", openAPIA10Root, d.Paths[openAPIA10Root]["post"], openAPIA10Case{status: 200, body: `{"name":"Priority","priority":-10,"error_codes":[429],"keywords":["Needle"],"match_mode":"all","platforms":["CUSTOM"]}`, change: func(f *openAPIA10Fixture) { f.cache = &openAPIA10Cache{} }})
	if len(f.audit.logs) != 1 || f.repo.calls != 2 {
		t.Fatal("write audit or post-write List sequence drift")
	}
	if hit := f.svc.MatchRule("custom", 429, []byte("a NEEDLE b")); hit == nil || hit.ID != 901 {
		t.Fatal("new rule not visible to synthetic matcher")
	}
	if hit := f.svc.MatchRule("custom", 429, []byte("nothing")); hit != nil {
		t.Fatal("all incorrectly matched one condition")
	}
	f.repo.rows[901].Enabled = false
	f.cache.callback()
	if hit := f.svc.MatchRule("custom", 429, []byte("NEEDLE")); hit != nil {
		t.Fatal("synchronous notification failed")
	}
	path := openAPIA10Root + "/{id}"
	g, _ := openAPIA10Run(t, "put", path, d.Paths[path]["put"], openAPIA10Case{status: 200, body: `{"enabled":false}`, change: func(f *openAPIA10Fixture) { f.repo.fail = "list" }})
	if hit := g.svc.MatchRule("custom", 429, []byte("Busy")); hit != nil {
		t.Fatal("stale cache retained after reload failure")
	}
}

func openAPIA10ResponseClone(t *testing.T, b map[string]any) map[string]any {
	t.Helper()
	raw, e := json.Marshal(b)
	if e != nil {
		t.Fatal(e)
	}
	var c map[string]any
	if e = openAPIDecode(raw, &c); e != nil {
		t.Fatal(e)
	}
	return c
}
