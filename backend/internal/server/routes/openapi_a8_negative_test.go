package routes_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIA8CoverageAndNegatives(t *testing.T) {
	doc, inv := openAPILoad(t)
	count := 0
	for _, row := range inv.Normal {
		if openAPIA8Operation(row.Method + " " + openAPIPath(row.Path)) {
			count++
			if row.Coverage != "covered" || row.Authentication != "admin-jwt-or-key" {
				t.Fatal("coverage metadata drift")
			}
		}
	}
	if count != 15 {
		t.Fatalf("module=%d want=15", count)
	}
	for _, key := range openAPIA8Routes {
		t.Run(key, func(t *testing.T) {
			method, path, _ := strings.Cut(key, " ")
			f := openAPIA8NewFixture(t)
			id := "301"
			if strings.Contains(path, "templates") {
				id = "501"
			}
			if strings.HasSuffix(path, "/run") {
				m := f.monitors.rows[301]
				m.CheckMode = "quota"
				m.AccountID = nil
				f.monitors.rows[301] = m
			}
			r := f.router(t)
			status := 200
			if method == "POST" && !strings.Contains(path, "{id}") {
				status = 201
			}
			_, b := openAPIA8Call(t, r, method, path, id, openAPIA8Body(strings.ToLower(method), path), "", "", status, false)
			op := doc.Paths[path][strings.ToLower(method)]
			s := op.Responses[map[bool]string{true: "201", false: "200"}[status == 201]].Content["application/json"].Schema
			for _, mutation := range []string{"missing code", "wrong code", "extra envelope", "extra data", "wrong data", "missing data field"} {
				t.Run(mutation, func(t *testing.T) {
					raw, _ := json.Marshal(b)
					var c map[string]any
					if e := openAPIDecode(raw, &c); e != nil {
						t.Fatal(e)
					}
					switch mutation {
					case "missing code":
						delete(c, "code")
					case "wrong code":
						c["code"] = json.Number("1")
					case "extra envelope":
						c["api_key"] = "synthetic"
					case "extra data":
						if data, ok := c["data"].(map[string]any); ok {
							data["api_key"] = "synthetic"
						} else {
							c["data"] = map[string]any{}
						}
					case "wrong data":
						c["data"] = true
					case "missing data field":
						if data, ok := c["data"].(map[string]any); ok {
							for k := range data {
								delete(data, k)
							}
						} else {
							delete(c, "message")
						}
					}
					if e := openAPIValidateValue(s, c); e == nil {
						t.Fatal("invalid response accepted")
					}
				})
			}
			for _, status := range []string{"400", "401", "403", "404", "409", "423", "429", "500", "503"} {
				if response, ok := op.Responses[status]; ok {
					if openAPIValidateValue(response.Content["application/json"].Schema, map[string]any{"code": true, "message": "synthetic"}) == nil {
						t.Fatal("invalid error type accepted")
					}
				}
			}
		})
	}
}
