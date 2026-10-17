package routes_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// Only the observed form/explode query-list wire grammar is supported. Scalar
// behavior remains first-value string validation, as in the accepted profile.
func openAPICheckParameter(p openAPIParameter) error {
	if err := openAPICheckSchema(p.Schema); err != nil {
		return err
	}
	if p.Schema["type"] != "array" {
		if p.Style != "" || p.Explode != nil || p.QueryList != "" {
			return fmt.Errorf("unsupported scalar serialization")
		}
		return nil
	}
	if p.In != "query" || p.Style != "form" || p.Explode == nil || !*p.Explode || p.QueryList != "comma-trim-empty" {
		return fmt.Errorf("unsupported array parameter serialization")
	}
	item, ok := p.Schema["items"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing item schema")
	}
	switch item["type"] {
	case "string":
	case "integer":
		if item["minimum"] == nil || item["maximum"] == nil {
			return fmt.Errorf("unsupported query integer binding")
		}
	default:
		return fmt.Errorf("unsupported query item type")
	}
	return nil
}
func openAPIValidateQueryList(p openAPIParameter, query url.Values) error {
	if err := openAPICheckParameter(p); err != nil {
		return err
	}
	raw, ok := query[p.Name]
	if !ok && p.Required {
		return fmt.Errorf("missing parameter %s", p.Name)
	}
	item, ok := p.Schema["items"].(map[string]any)
	if !ok {
		return fmt.Errorf("missing item schema")
	}
	values := []any{}
	for _, v := range raw {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			var value any = part
			if item["type"] == "integer" {
				n, err := strconv.ParseInt(part, 10, 64)
				if err != nil {
					return fmt.Errorf("%s: invalid int64", p.Name)
				}
				value = json.Number(strconv.FormatInt(n, 10))
			}
			values = append(values, value)
		}
	}
	return openAPIValidateValue(p.Schema, values)
}
func TestOpenAPIA5QueryLists(t *testing.T) {
	doc, _ := openAPILoad(t)
	op := doc.Paths["/api/v1/channel-monitor-v2/models"]["get"]
	for _, p := range op.Parameters {
		if p.Schema["type"] != "array" {
			continue
		}
		t.Run(p.Name, func(t *testing.T) {
			cases := []struct {
				raw   string
				valid bool
			}{{"", true}, {p.Name + "=,%20,,&" + p.Name + "=", true}}
			if p.Name == "group_id" {
				cases = append(cases, struct {
					raw   string
					valid bool
				}{"group_id=13,0011&group_id=%2B14,13&group_id=9223372036854775807", true})
				for _, v := range []string{"0", "-1", "1.5", "1e1", "bad", "9223372036854775808", "-9223372036854775809"} {
					cases = append(cases, struct {
						raw   string
						valid bool
					}{"group_id=13&group_id=" + v, false})
				}
			} else {
				cases = append(cases, struct {
					raw   string
					valid bool
				}{p.Name + "=unknown,%20MixedCase%20&" + p.Name + "=a%2Fb,unknown", true})
			}
			for _, tc := range cases {
				t.Run(tc.raw, func(t *testing.T) {
					q, err := url.ParseQuery(tc.raw)
					if err != nil {
						t.Fatal(err)
					}
					if err := openAPIValidateQueryList(p, q); (err == nil) != tc.valid {
						t.Fatalf("valid=%v error=%v", tc.valid, err)
					}
				})
			}
			for _, name := range []string{"no style", "false explode", "unknown splitter", "wrong location", "object item", "unbounded integer binding"} {
				t.Run(name, func(t *testing.T) {
					q := p
					switch name {
					case "no style":
						q.Style = ""
					case "false explode":
						v := false
						q.Explode = &v
					case "unknown splitter":
						q.QueryList = "anything"
					case "wrong location":
						q.In = "header"
					case "object item":
						q.Schema = map[string]any{"type": "array", "items": map[string]any{"type": "boolean"}}
					case "unbounded integer binding":
						q.Schema = map[string]any{"type": "array", "items": map[string]any{"type": "integer"}}
					}
					if openAPICheckParameter(q) == nil {
						t.Fatal("unsupported serialization accepted")
					}
				})
			}
		})
	}
	old := doc
	old.Profile = "a3-usage-request-v5"
	_, inv := openAPILoad(t)
	if openAPIValidateDocument(old, inv.Normal) == nil {
		t.Fatal("stale profile accepted")
	}
}
