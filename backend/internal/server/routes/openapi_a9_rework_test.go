package routes_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Only APIKeyService's post-authentication user lookup is substituted. JWT
// middleware still uses the original user service/repository through SetupRouter.
type openAPIA9AvailabilityUsers struct {
	service.UserRepository
	err error
	ids []int64
}

func (r *openAPIA9AvailabilityUsers) GetByID(ctx context.Context, id int64) (*service.User, error) {
	r.ids = append(r.ids, id)
	if r.err != nil {
		return nil, r.err
	}
	return r.UserRepository.GetByID(ctx, id)
}
func openAPIA9PostAuthUserCases() []openAPIA9Case {
	return []openAPIA9Case{
		{name: "post-auth user missing", status: 404, reason: "USER_NOT_FOUND", change: func(f *openAPIA9Fixture) {
			f.availableUsers = &openAPIA9AvailabilityUsers{err: service.ErrUserNotFound}
		}, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
			openAPIA9InspectUserLookup(t, f, 1)
			if b["code"] != json.Number("404") || b["message"] != "user not found" {
				t.Fatal("wrapped USER_NOT_FOUND envelope drift")
			}
		}},
		{name: "post-auth user storage failure", status: 500, change: func(f *openAPIA9Fixture) {
			f.availableUsers = &openAPIA9AvailabilityUsers{err: fmt.Errorf("PRIVATE-A9-POST-AUTH")}
		}, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
			openAPIA9InspectUserLookup(t, f, 1)
			if b["code"] != json.Number("500") {
				t.Fatal("post-auth generic failure must use numeric code500")
			}
		}},
	}
}
func openAPIA9InspectUserLookup(t *testing.T, f *openAPIA9Fixture, want int) {
	t.Helper()
	if f.availableUsers == nil || len(f.availableUsers.ids) != want {
		t.Fatalf("post-auth lookup count differs from %d", want)
	}
	for _, id := range f.availableUsers.ids {
		if id != 41 {
			t.Fatalf("post-auth lookup user=%d want JWT subject41", id)
		}
	}
	if f.repo.calls != 0 || f.repo.writes != 0 {
		t.Fatal("short circuit reached channel repository")
	}
}
func TestOpenAPIA9PostAuthUserErrors(t *testing.T) {
	doc, _ := openAPILoad(t)
	path := "/api/v1/channels/available"
	op := doc.Paths[path]["get"]
	cases := openAPIA9PostAuthUserCases()
	cases = append(cases,
		openAPIA9Case{name: "auth-stage user missing", status: 401, change: func(f *openAPIA9Fixture) {
			f.a1.users.lookupErr = service.ErrUserNotFound
			f.availableUsers = &openAPIA9AvailabilityUsers{err: service.ErrUserNotFound}
		}, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
			openAPIA9InspectUserLookup(t, f, 0)
			if b["code"] != "USER_NOT_FOUND" {
				t.Fatal("auth-stage not-found must be401 with string code")
			}
		}},
		openAPIA9Case{name: "auth-stage storage failure", status: 500, change: func(f *openAPIA9Fixture) {
			f.a1.users.lookupErr = fmt.Errorf("PRIVATE-A9-AUTH")
			f.availableUsers = &openAPIA9AvailabilityUsers{err: service.ErrUserNotFound}
		}, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
			openAPIA9InspectUserLookup(t, f, 0)
			if b["code"] != "INTERNAL_ERROR" {
				t.Fatal("auth-stage generic failure must have string INTERNAL_ERROR")
			}
		}},
	)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { openAPIA9Run(t, "get", path, op, tc) })
	}
	for _, lookupErr := range []error{service.ErrUserNotFound, fmt.Errorf("PRIVATE-A9-POST-AUTH")} {
		for _, noAuth := range []bool{false, true} {
			t.Run(fmt.Sprintf("disabled lookup=%v no-auth=%t", lookupErr, noAuth), func(t *testing.T) {
				tc := openAPIA9Case{status: 200, change: func(f *openAPIA9Fixture) {
					f.a1.settings.values[service.SettingKeyAvailableChannelsEnabled] = "false"
					f.availableUsers = &openAPIA9AvailabilityUsers{err: lookupErr}
				}, inspect: func(t *testing.T, f *openAPIA9Fixture, b map[string]any) {
					openAPIA9InspectUserLookup(t, f, 0)
					if !noAuth && len(openAPIA5Array(t, b["data"])) != 0 {
						t.Fatal("disabled feature must return200 []")
					}
				}}
				if noAuth {
					tc.status = 401
					tc.auth = "-"
				}
				openAPIA9Run(t, "get", path, op, tc)
			})
		}
	}
	// The new error shape stays closed and distinguishes auth code strings from
	// the numeric wrapped service error after authentication.
	_, b := openAPIA9Run(t, "get", path, op, openAPIA9PostAuthUserCases()[0])
	schema := op.Responses["404"].Content["application/json"].Schema
	for _, kind := range []string{"wrong status code", "wrong reason", "missing reason", "extra private field"} {
		t.Run(kind, func(t *testing.T) {
			c := openAPIA8Clone(b)
			switch kind {
			case "wrong status code":
				c["code"] = "USER_NOT_FOUND"
			case "wrong reason":
				c["reason"] = "CHANNEL_NOT_FOUND"
			case "missing reason":
				delete(c, "reason")
			case "extra private field":
				c["credential"] = "synthetic"
			}
			if openAPIValidateValue(schema, c) == nil {
				t.Fatal("incorrect404 envelope accepted")
			}
		})
	}
}
