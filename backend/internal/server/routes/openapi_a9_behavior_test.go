package routes_test

import (
	"encoding/json"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAPIA9WriteSemantics(t *testing.T) {
	doc, _ := openAPILoad(t)
	for _, method := range []string{"post", "put"} {
		path := "/api/v1/admin/channels"
		if method == "put" {
			path += "/{id}"
		}
		op := doc.Paths[path][method]
		for _, tc := range []openAPIA9Case{
			{name: "full nested DTO", status: 200, body: `{"name":" New ","description":" detail ","status":"disabled","billing_model_source":"response_model","restrict_models":true,"features":"plain text","features_config":{"nested":[null,true,1.25,{"arbitrary":"value"}]},"group_ids":[13],"model_mapping":{"anthropic":{"alias":"synthetic"},"openai":null},"model_pricing":[{"models":["synthetic"],"input_price":0,"cache_write_1h_price":0.2,"fast_multiplier":2,"flex_multiplier":0.5,"reasoning_effort_multipliers":{"high":1.5},"intervals":[{"min_tokens":0,"max_tokens":100,"input_multiplier":2,"cache_write_1h_price":0.1,"sort_order":7}],"time_pricing":{"timezone":"UTC","weekdays_only":true,"periods":[{"start_time":"08:00","end_time":"09:00","multiplier":2}]}}],"apply_pricing_to_account_stats":true,"account_stats_pricing_rules":[{"name":"stats","account_ids":[5],"pricing":[{"models":["stats-model"],"input_price":0.4,"fast_multiplier":3,"flex_multiplier":0.2,"intervals":[{"min_tokens":0,"input_price":0.3,"input_multiplier":9}]}]}],"ignored":42}`, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
				id := int64(701)
				if method == "put" {
					id = 301
				}
				c := f.repo.rows[id]
				if c.Name != " New " || c.Description != " detail " || c.BillingModelSource != "response_model" || !c.RestrictModels || c.Features != "plain text" || !c.ApplyPricingToAccountStats {
					t.Fatal("scalar preservation/default drift")
				}
				p := c.ModelPricing[0]
				if p.Platform != "anthropic" || p.BillingMode != service.BillingModeToken || p.InputPrice == nil || *p.InputPrice != 0 || p.Intervals[0].InputMultiplier == nil {
					t.Fatal("nested pricing drift")
				}
				sp := c.AccountStatsPricingRules[0].Pricing[0]
				if sp.FastMultiplier != nil || sp.FlexMultiplier != nil || sp.Intervals[0].InputMultiplier != nil {
					t.Fatal("account stats leaked channel multipliers")
				}
				if method == "post" && c.Status != "active" {
					t.Fatal("unknown status should not override create")
				}
				if f.repo.writes != 1 {
					t.Fatal("write count drift")
				}
			}},
			{name: "null nested fields", status: 200, body: `{"name":"Created","description":null,"group_ids":null,"model_pricing":null,"model_mapping":null,"features_config":null,"restrict_models":null,"account_stats_pricing_rules":null}`},
			{name: "whitespace name retained", status: 200, body: `{"name":"   "}`, inspect: func(t *testing.T, _ *openAPIA9Fixture, b map[string]any) {
				if openAPIA5Map(t, b["data"])["name"] != "   " {
					t.Fatal("name trimmed")
				}
			}},
			{name: "typed map rejects numbers", status: 400, invalid: true, body: `{"name":"Created","model_mapping":{"openai":{"x":1}}}`},
			{name: "group integer token", status: 400, invalid: true, body: `{"name":"Created","group_ids":[1.0]}`},
			{name: "interval integer token", status: 400, invalid: true, body: `{"name":"Created","model_pricing":[{"intervals":[{"min_tokens":1e0}]}]}`},
			{name: "negative flat price", reason: "NEGATIVE_PRICE", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"input_price":-1}]}`},
			{name: "zero fast multiplier", reason: "INVALID_MULTIPLIER", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"fast_multiplier":0}]}`},
			{name: "unsupported effort", reason: "INVALID_REASONING_EFFORT_MULTIPLIER", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"reasoning_effort_multipliers":{"ultra":2}}]}`},
			{name: "zero effort", reason: "INVALID_REASONING_EFFORT_MULTIPLIER", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"reasoning_effort_multipliers":{"high":0}}]}`},
			{name: "price required image", reason: "BILLING_MODE_MISSING_PRICE", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"billing_mode":"image"}]}`},
			{name: "per request price", status: 200, body: `{"name":"Created","model_pricing":[{"models":["x"],"billing_mode":"per_request","per_request_price":0.5}]}`},
			{name: "overlapping models", reason: "MODEL_PATTERN_CONFLICT", status: 400, body: `{"name":"Created","model_pricing":[{"models":["X*","xyz"]}]}`},
			{name: "overlapping mapping", reason: "MAPPING_PATTERN_CONFLICT", status: 400, body: `{"name":"Created","model_mapping":{"openai":{"X*":"a","xyz":"b"}}}`},
			{name: "cross platform independent", status: 200, body: `{"name":"Created","model_pricing":[{"platform":"openai","models":["x"]},{"platform":"anthropic","models":["x"]}]}`},
			{name: "interval negative bound", reason: "INVALID_PRICING_INTERVALS", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"intervals":[{"min_tokens":-1,"input_price":1}]}]}`},
			{name: "interval inverted", reason: "INVALID_PRICING_INTERVALS", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"intervals":[{"min_tokens":2,"max_tokens":1,"input_price":1}]}]}`},
			{name: "interval overlap", reason: "INVALID_PRICING_INTERVALS", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"intervals":[{"min_tokens":0,"max_tokens":10,"input_price":1},{"min_tokens":5,"input_price":2}]}]}`},
			{name: "empty interval price", reason: "INTERVAL_MISSING_PRICE", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"intervals":[{"min_tokens":0}]}]}`},
			{name: "negative interval price", reason: "INVALID_PRICING_INTERVALS", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"intervals":[{"min_tokens":0,"input_price":-1}]}]}`},
			{name: "time unsupported mode", reason: "TIME_PRICING_UNSUPPORTED_MODE", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"billing_mode":"image","per_request_price":1,"time_pricing":{"timezone":"UTC","periods":[{"start_time":"08:00","end_time":"09:00","multiplier":2}]}}]}`},
			{name: "invalid time zone", reason: "INVALID_TIME_PRICING", status: 400, body: `{"name":"Created","model_pricing":[{"models":["x"],"time_pricing":{"timezone":"Invalid/Zone","periods":[{"start_time":"08:00","end_time":"09:00","multiplier":2}]}}]}`},
			{name: "empty time clears", status: 200, body: `{"name":"Created","model_pricing":[{"models":["x"],"time_pricing":{"timezone":"ignored","periods":[]}}]}`, inspect: func(t *testing.T, _ *openAPIA9Fixture, b map[string]any) {
				p := openAPIA5Map(t, openAPIA5Array(t, openAPIA5Map(t, b["data"])["model_pricing"])[0])
				if p["time_pricing"] != nil {
					t.Fatal("empty schedule not cleared")
				}
			}},
			{name: "stats scope required", reason: "PRICING_RULE_EMPTY_SCOPE", status: 400, body: `{"name":"Created","account_stats_pricing_rules":[{"pricing":[{"models":["x"]}]}]}`},
			{name: "stats pricing required", reason: "PRICING_RULE_EMPTY_PRICING", status: 400, body: `{"name":"Created","account_stats_pricing_rules":[{"group_ids":[13]}]}`},
			{name: "stats time unsupported", reason: "ACCOUNT_STATS_TIME_PRICING_UNSUPPORTED", status: 400, body: `{"name":"Created","account_stats_pricing_rules":[{"group_ids":[13],"pricing":[{"models":["x"],"time_pricing":{"timezone":"UTC","periods":[{"start_time":"08:00","end_time":"09:00","multiplier":2}]}}]}]}`},
			{name: "nested binding absence", status: 200, body: `{"name":"Created","model_pricing":[{"models":[],"billing_mode":"legacy-custom","platform":"legacy-platform"}]}`},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) { openAPIA9Run(t, method, path, op, tc) })
		}
	}
	for _, body := range []string{`{}`, `null`, `{"name":"","description":null,"status":"","billing_model_source":"","features_config":null,"model_pricing":null,"group_ids":null,"model_mapping":null}`} {
		t.Run("put retain "+body, func(t *testing.T) {
			op := doc.Paths["/api/v1/admin/channels/{id}"]["put"]
			f, _ := openAPIA9Run(t, "put", "/api/v1/admin/channels/{id}", op, openAPIA9Case{body: body, status: 200})
			c := f.repo.rows[301]
			if c.Name != "Visible" || c.Description != "Synthetic description" || len(c.ModelPricing) != 1 || len(c.GroupIDs) != 1 || len(c.ModelMapping) != 1 || len(c.FeaturesConfig) != 1 {
				t.Fatal("null/omitted replacement drift")
			}
		})
	}
	t.Run("put clear", func(t *testing.T) {
		op := doc.Paths["/api/v1/admin/channels/{id}"]["put"]
		f, b := openAPIA9Run(t, "put", "/api/v1/admin/channels/{id}", op, openAPIA9Case{status: 200, body: `{"description":"","features":"","features_config":{},"group_ids":[],"model_mapping":{},"model_pricing":[],"account_stats_pricing_rules":[],"restrict_models":false,"apply_pricing_to_account_stats":false}`})
		c := f.repo.rows[301]
		if c.Description != "" || c.Features != "" || len(c.ModelPricing)+len(c.GroupIDs)+len(c.ModelMapping)+len(c.FeaturesConfig) != 0 {
			t.Fatal("empty clears drift")
		}
		data := openAPIA5Map(t, b["data"])
		for _, k := range []string{"model_pricing", "group_ids", "account_stats_pricing_rules"} {
			if len(openAPIA5Array(t, data[k])) != 0 {
				t.Fatal("empty array shape")
			}
		}
	})
}
func TestOpenAPIA9UserVisibility(t *testing.T) {
	doc, _ := openAPILoad(t)
	path := "/api/v1/channels/available"
	op := doc.Paths[path]["get"]
	for _, tc := range []struct {
		name   string
		change func(*openAPIA9Fixture)
		want   int
	}{
		{"normal", func(f *openAPIA9Fixture) { f.a1.users.user.Role = service.RoleUser }, 1},
		{"inactive channel", func(f *openAPIA9Fixture) { c := f.repo.rows[301]; c.Status = "disabled"; f.repo.rows[301] = c }, 0},
		{"no active groups", func(f *openAPIA9Fixture) { f.groups.groups = nil }, 0},
		{"foreign exclusive", func(f *openAPIA9Fixture) { f.groups.groups[0].IsExclusive = true }, 0},
		{"allowed exclusive", func(f *openAPIA9Fixture) {
			f.groups.groups[0].IsExclusive = true
			f.a1.users.user.AllowedGroups = []int64{13}
		}, 1},
		{"subscription absent", func(f *openAPIA9Fixture) { f.groups.groups[0].SubscriptionType = "subscription" }, 0},
		{"subscription active", func(f *openAPIA9Fixture) {
			f.groups.groups[0].SubscriptionType = "subscription"
			f.subs.groups[13] = true
		}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, b := openAPIA9Run(t, "get", path, op, openAPIA9Case{status: 200, change: tc.change})
			if len(openAPIA5Array(t, b["data"])) != tc.want {
				t.Fatal("visibility drift")
			}
		})
	}
	for _, composite := range []bool{false, true} {
		t.Run(fmtBool(composite), func(t *testing.T) {
			_, b := openAPIA9Run(t, "get", path, op, openAPIA9Case{status: 200, headers: map[string]string{"Accept-Language": "en"}, change: func(f *openAPIA9Fixture) {
				c := f.repo.rows[301]
				p := c.ModelPricing[0].Clone()
				p.Platform = "openai"
				p.Models = []string{"second-platform"}
				c.ModelPricing = append(c.ModelPricing, p)
				c.ModelMapping["anthropic"]["unpriced"] = "not-priced"
				c.ModelPricing = append(c.ModelPricing, service.ChannelModelPricing{Platform: "anthropic", Models: []string{"wild*"}})
				f.repo.rows[301] = c
				if composite {
					f.groups.groups[0].Platform = "composite"
				}
			}})
			channels := openAPIA5Array(t, b["data"])
			ch := openAPIA5Map(t, channels[0])
			sections := openAPIA5Array(t, ch["platforms"])
			want := 1
			if composite {
				want = 2
			}
			if len(sections) != want {
				t.Fatal("platform isolation drift")
			}
			for _, section := range sections {
				s := openAPIA5Map(t, section)
				gs := openAPIA5Array(t, s["groups"])
				if openAPIA5Map(t, gs[0])["name"] != "Translated" {
					t.Fatal("language drift")
				}
				for _, raw := range openAPIA5Array(t, s["supported_models"]) {
					m := openAPIA5Map(t, raw)
					if strings.Contains(fmtString(t, m["name"]), "*") {
						t.Fatal("wildcard leaked")
					}
					if m["platform"] != s["platform"] {
						t.Fatal("cross-platform model leaked")
					}
					if m["name"] == "alias-a9" && m["pricing"] == nil {
						t.Fatal("target pricing missing")
					}
					if m["name"] == "unpriced" && m["pricing"] != nil {
						t.Fatal("invented price")
					}
				}
			}
		})
	}
}
func fmtBool(v bool) string {
	if v {
		return "composite"
	}
	return "ordinary"
}
func fmtString(t *testing.T, v any) string {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Fatalf("not string: %T", v)
	}
	return s
}
func TestOpenAPIA9CoverageAndNegatives(t *testing.T) {
	doc, inv := openAPILoad(t)
	count := 0
	for _, row := range inv.Normal {
		if openAPIA9Operation(row.Method + " " + openAPIPath(row.Path)) {
			count++
			if row.Coverage != "covered" {
				t.Fatal("coverage drift")
			}
		}
	}
	if count != 8 {
		t.Fatalf("count=%d", count)
	}
	for _, key := range openAPIA9Routes {
		t.Run(key, func(t *testing.T) {
			method, path, _ := strings.Cut(key, " ")
			method = strings.ToLower(method)
			op := doc.Paths[path][method]
			_, b := openAPIA9Run(t, method, path, op, openAPIA9Case{status: 200})
			s := op.Responses["200"].Content["application/json"].Schema
			for _, kind := range []string{"missing code", "wrong code", "extra envelope", "wrong data", "missing data"} {
				t.Run(kind, func(t *testing.T) {
					c := openAPIA8Clone(b)
					switch kind {
					case "missing code":
						delete(c, "code")
					case "wrong code":
						c["code"] = json.Number("1")
					case "extra envelope":
						c["private"] = true
					case "wrong data":
						c["data"] = true
					case "missing data":
						delete(c, "data")
					}
					if openAPIValidateValue(s, c) == nil {
						t.Fatal("invalid response accepted")
					}
				})
			}
			for status, r := range op.Responses {
				if status != "200" && openAPIValidateValue(r.Content["application/json"].Schema, map[string]any{"code": true, "message": "invalid"}) == nil {
					t.Fatal("error type accepted")
				}
			}
			if method == "get" && path == "/api/v1/admin/channels/{id}" {
				data := openAPIA5Map(t, b["data"])
				for _, k := range []string{"id", "group_ids", "features_config", "model_mapping", "model_pricing", "account_stats_pricing_rules"} {
					c := openAPIA8Clone(b)
					openAPIA5Map(t, c["data"])[k] = true
					if openAPIValidateValue(s, c) == nil {
						t.Fatalf("type mutation accepted %s", k)
					}
				}
				if !reflect.DeepEqual(data["billing_model_source"], "channel_mapped") {
					t.Fatal("legacy billing source not normalized")
				}
			}
		})
	}
}

func TestOpenAPIA9FeatureFailureAndCatalog(t *testing.T) {
	d, _ := openAPILoad(t)
	path := "/api/v1/channels/available"
	_, b := openAPIA9Run(t, "get", path, d.Paths[path]["get"], openAPIA9Case{status: 200, change: func(f *openAPIA9Fixture) { f.a1.settings.multipleErr = true }})
	if len(openAPIA5Array(t, b["data"])) != 0 {
		t.Fatal("setting failure did not fail closed")
	}
	path = "/api/v1/admin/channels/pricing/sync-models"
	for _, platform := range []string{"anthropic", "antigravity", "openai", "gemini", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "typesafe", "command_code", "cline"} {
		t.Run(platform, func(t *testing.T) {
			status := 200
			if platform == "command_code" || platform == "cline" {
				status = 400
			}
			openAPIA9Run(t, "get", path, d.Paths[path]["get"], openAPIA9Case{status: status, query: "?platform=" + platform})
		})
	}
}
func TestOpenAPIA9UserWhitelistNegatives(t *testing.T) {
	d, _ := openAPILoad(t)
	path := "/api/v1/channels/available"
	op := d.Paths[path]["get"]
	_, b := openAPIA9Run(t, "get", path, op, openAPIA9Case{status: 200})
	s := op.Responses["200"].Content["application/json"].Schema
	for _, level := range []string{"channel", "group", "model", "pricing", "interval"} {
		t.Run(level, func(t *testing.T) {
			c := openAPIA8Clone(b)
			ch := openAPIA5Map(t, openAPIA5Array(t, c["data"])[0])
			section := openAPIA5Map(t, openAPIA5Array(t, ch["platforms"])[0])
			model := openAPIA5Map(t, openAPIA5Array(t, section["supported_models"])[0])
			p := openAPIA5Map(t, model["pricing"])
			target := ch
			switch level {
			case "group":
				target = openAPIA5Map(t, openAPIA5Array(t, section["groups"])[0])
			case "model":
				target = model
			case "pricing":
				target = p
			case "interval":
				target = openAPIA5Map(t, openAPIA5Array(t, p["intervals"])[0])
			}
			target["internal_secret"] = true
			if openAPIValidateValue(s, c) == nil {
				t.Fatal("field whitelist leak accepted")
			}
		})
	}
}
