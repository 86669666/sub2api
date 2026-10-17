package routes_test

import (
	"encoding/json"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"net/url"
	"strings"
	"testing"
	"time"
)

func openAPIA7QueryCases(path string) []openAPIA7Case {
	cases := []openAPIA7Case{
		{name: "nil analytics result", status: 200, change: func(f *openAPIA7Fixture) { f.v2.nilResult = true }},
		{name: "empty", status: 200, change: func(f *openAPIA7Fixture) { f.v2.empty = true }},
		{name: "analytics failure", status: 500, change: func(f *openAPIA7Fixture) { f.v2.fail = true }},
		{name: "privacy flags ignored", status: 200, change: func(f *openAPIA7Fixture) {
			f.a1.settings.values[service.SettingKeyChannelMonitorHideThroughput] = "true"
			f.a1.settings.values[service.SettingKeyChannelMonitorHideUserRanking] = "true"
		}},
		{name: "privacy reader absent", status: 200, change: func(f *openAPIA7Fixture) { f.noRuntimeReader = true }},
		{name: "lists normalize", query: "?platform=OpenAI,%20openai,,&platform=openai&model=Z,a&model=a&group_id=11,%2B099&group_id=0011", status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			v := f.v2.filter
			if fmt.Sprint(v.Platforms) != "[OpenAI openai]" || fmt.Sprint(v.Models) != "[Z a]" || fmt.Sprint(v.GroupIDs) != "[11 99]" {
				t.Fatalf("filter drift %+v", v)
			}
		}},
		{name: "empty tokens", query: "?group_id=,,&group_id=%20&model=,,&platform=,,", status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			if len(f.v2.filter.GroupIDs)+len(f.v2.filter.Models)+len(f.v2.filter.Platforms) != 0 {
				t.Fatal("empty tokens drift")
			}
		}},
		{name: "max int64", query: "?group_id=9223372036854775807", status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			if fmt.Sprint(f.v2.filter.GroupIDs) != "[9223372036854775807]" {
				t.Fatal("int64 drift")
			}
		}},
		{name: "first range", query: "?range=24h&range=bad", status: 200},
		{name: "invalid first range", query: "?range=bad&range=24h", status: 400},
	}
	for _, v := range []string{"", "90m", "24h", "7d", "30d", " 24h "} {
		cases = append(cases, openAPIA7Case{name: "range " + v, query: "?range=" + url.QueryEscape(v), status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			window, bucket := 90*time.Minute, 5*time.Minute
			switch strings.TrimSpace(v) {
			case "24h":
				window = 24 * time.Hour
				bucket = time.Hour
			case "7d":
				window = 7 * 24 * time.Hour
				bucket = 12 * time.Hour
			case "30d":
				window = 30 * 24 * time.Hour
				bucket = 24 * time.Hour
			}
			q := f.v2.filter
			if q.End.Sub(q.Start) != window || q.Bucket != bucket || !q.End.Equal(q.End.Truncate(bucket)) {
				t.Fatal("window drift")
			}
		}})
	}
	for _, v := range []string{"90M", "24H", "1h", "bad"} {
		cases = append(cases, openAPIA7Case{name: "invalid range " + v, query: "?range=" + v, status: 400})
	}
	for _, v := range []string{"0", "-1", "bad", "1.5", "1e2", "9223372036854775808", "-9223372036854775809"} {
		cases = append(cases, openAPIA7Case{name: "bad group " + v, query: "?group_id=11&group_id=" + v, status: 400, invalidRequest: true})
	}
	if strings.HasSuffix(path, "/matrix") {
		for _, v := range []string{"", "platform", "platform_group", "platform_model", "platform_group_model", " platform "} {
			cases = append(cases, openAPIA7Case{name: "group_by " + v, query: "?group_by=" + url.QueryEscape(v) + "&group_by=bad", status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
				want := strings.TrimSpace(v)
				if want == "" {
					want = "platform_group"
				}
				if string(f.v2.groupBy) != want {
					t.Fatal("group_by drift")
				}
			}})
		}
		for _, v := range []string{"PLATFORM", "bad"} {
			cases = append(cases, openAPIA7Case{name: "bad group_by " + v, query: "?group_by=" + v, status: 400})
		}
	} else {
		cases = append(cases, openAPIA7Case{name: "group_by ignored", query: "?group_by=bad", status: 200})
	}
	if strings.HasSuffix(path, "/users") {
		cases = append(cases, openAPIA7Case{name: "top ten plus self", status: 200, change: func(f *openAPIA7Fixture) { f.v2.manyUsers = true }, inspect: func(t *testing.T, _ *openAPIA7Fixture, v any) {
			rows := openAPIA5Array(t, openAPIA5Map(t, v)["items"])
			last := openAPIA5Map(t, rows[len(rows)-1])
			if len(rows) != 11 || last["rank"] != json.Number("13") || last["user_id"] != json.Number("41") || last["is_self"] != true {
				t.Fatal("top self drift")
			}
		}}, openAPIA7Case{name: "missing self", status: 200, change: func(f *openAPIA7Fixture) { f.v2.missingSelf = true; f.v2.manyUsers = true }, inspect: func(t *testing.T, _ *openAPIA7Fixture, v any) {
			rows := openAPIA5Array(t, openAPIA5Map(t, v)["items"])
			last := openAPIA5Map(t, rows[len(rows)-1])
			if len(rows) != 11 || last["rank"] != json.Number("0") || last["display_label"] != "Me" || last["can_drilldown"] != true {
				t.Fatal("missing self drift")
			}
		}})
	}
	return cases
}
func openAPIA7WriteCases() []openAPIA7Case {
	cases := []openAPIA7Case{
		{name: "conflict", status: 409, change: func(f *openAPIA7Fixture) { f.v2.conflict = true }},
		{name: "zero defaults", body: `{}`, status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			c := f.v2.input
			if c.RefreshIntervalSeconds != 300 || c.Enabled || c.Platforms != nil || c.GroupIDs == nil || c.IgnoredErrorCategories == nil || c.HealthThresholds != service.DefaultChannelMonitorV2HealthThresholds() || f.v2.expected != 0 {
				t.Fatal("default replacement drift")
			}
		}},
		{name: "root null", body: `null`, status: 200},
		{name: "nullable fields", body: `{"version":null,"enabled":null,"platforms":null,"group_ids":null,"health_thresholds":null,"ignored_error_categories":null,"updated_at":null,"updated_by":null}`, status: 200},
		{name: "unknown nested fields", body: `{"unknown":[],"platforms":[{"platform":" OpenAI ","unknown":true}],"health_thresholds":{"unknown":false},"updated_by":42}`, status: 200},
		{name: "normalize all", body: `{"version":-7,"enabled":true,"refresh_interval_seconds":60,"group_ids":[99,11,99,9223372036854775807],"platforms":[{"platform":" OpenAI ","enabled":true,"models":[" a ","a","A"," "]},{"platform":" ANTHROPIC ","models":null}],"ignored_error_categories":[" TIMEOUT ","timeout","WHAT","authentication"],"updated_at":"2001-01-02T03:04:05Z","updated_by":999}`, status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			c := f.v2.input
			if f.v2.expected != -7 || c.RefreshIntervalSeconds != 60 || fmt.Sprint(c.GroupIDs) != "[11 99 9223372036854775807]" || c.Platforms[0].Platform != "anthropic" || c.Platforms[0].Models == nil || fmt.Sprint(c.Platforms[1].Models) != "[A a]" || fmt.Sprint(c.IgnoredErrorCategories) != "[authentication timeout]" || c.UpdatedAt.Year() != 2001 {
				t.Fatalf("normalization drift %+v", c)
			}
		}},
		{name: "threshold clamps", body: `{"health_thresholds":{"minimum_sample":10001,"warning_error_rate":2,"critical_error_rate":0.1,"target_ttft_ms":4000,"warning_ttft_ms":3,"critical_ttft_ms":4,"warning_cache_rate":0.2,"critical_cache_rate":2,"error_weight":-5,"ttft_weight":2,"cache_weight":0}}`, status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			h := f.v2.input.HealthThresholds
			if h.MinimumSample != 10000 || h.WarningErrorRate != 2 || h.CriticalErrorRate != 2 || h.WarningTTFTMs != 4001 || h.CriticalTTFTMs != 4001 || h.WarningCacheRate != 0.2 || h.CriticalCacheRate != 0.2 || h.ErrorWeight != -5 || h.TTFTWeight != 2 {
				t.Fatalf("threshold clamp drift %+v", h)
			}
		}},
		{name: "negative defaults", body: `{"health_thresholds":{"minimum_sample":-1,"warning_error_rate":-1,"critical_error_rate":-1,"target_ttft_ms":-1,"warning_ttft_ms":-1,"critical_ttft_ms":-1,"warning_cache_rate":-1,"critical_cache_rate":-1,"error_weight":-1,"ttft_weight":-1,"cache_weight":-1}}`, status: 200, inspect: func(t *testing.T, f *openAPIA7Fixture, _ any) {
			if f.v2.input.HealthThresholds != service.DefaultChannelMonitorV2HealthThresholds() {
				t.Fatal("negative defaults drift")
			}
		}},
	}
	for _, body := range []string{`{"refresh_interval_seconds":1}`, `{"group_ids":[0]}`, `{"group_ids":[-1]}`, `{"group_ids":[null]}`, `{"platforms":[null]}`, `{"platforms":[{}]}`, `{"platforms":[{"platform":" "}]}`, `{"platforms":[{"platform":"OpenAI"},{"platform":" openai "}]}`} {
		cases = append(cases, openAPIA7Case{name: "semantic " + body, body: body, status: 400})
	}
	for _, body := range []string{`[]`, `true`, `"x"`, `{`, `{"version":1.0}`, `{"version":1e2}`, `{"version":"2"}`, `{"version":9223372036854775808}`, `{"enabled":1}`, `{"group_ids":[1.0]}`, `{"group_ids":["1"]}`, `{"group_ids":[1e2]}`, `{"platforms":{}}`, `{"platforms":[{"platform":2}]}`, `{"platforms":[{"platform":"a","models":[1]}]}`, `{"health_thresholds":{"minimum_sample":1e2}}`, `{"health_thresholds":{"target_ttft_ms":1.0}}`, `{"health_thresholds":{"warning_ttft_ms":"1"}}`, `{"health_thresholds":{"critical_ttft_ms":9223372036854775808}}`, `{"health_thresholds":{"warning_error_rate":"1"}}`, `{"ignored_error_categories":[1]}`, `{"updated_by":1.0}`, `{"updated_at":"yesterday"}`, `{"updated_at":1}`} {
		cases = append(cases, openAPIA7Case{name: "binding " + body, body: body, status: 400, invalidRequest: true})
	}
	return cases
}
