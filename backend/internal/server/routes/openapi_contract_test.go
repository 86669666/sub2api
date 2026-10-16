package routes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/setup"
	"github.com/gin-gonic/gin"
)

// This fixture calls the production composition root, not a parallel list of
// registrars. Auth seams are identifiable sentinels: we inspect their placement,
// not the implementation of token verification (covered by middleware tests).
// Every inventory probe aborts BEFORE any application middleware or handler.
type openAPISettings struct{ service.SettingRepository }

func (openAPISettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, errors.New("OpenAPI fixture: settings deliberately unavailable")
}
func openAPIJWT(c *gin.Context)         { c.AbortWithStatus(http.StatusUnauthorized) }
func openAPIOptionalJWT(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) }
func openAPIAdmin(c *gin.Context)       { c.AbortWithStatus(http.StatusUnauthorized) }
func openAPIKey(c *gin.Context)         { c.AbortWithStatus(http.StatusUnauthorized) }
func openAPIStepUp(c *gin.Context)      { c.AbortWithStatus(http.StatusUnauthorized) }
func openAPIAudit(c *gin.Context)       { c.Next() }

type openAPIRoute struct {
	Method         string `json:"method"`
	Path           string `json:"path"`
	Authentication string `json:"authentication"`
	StepUp         bool   `json:"step_up"`
}
type openAPICoverage struct {
	openAPIRoute
	Coverage string `json:"coverage"`
	Owner    string `json:"owner"`
	Reason   string `json:"reason"`
}
type openAPIInventory struct {
	Format    int               `json:"format"`
	Baseline  string            `json:"baseline"`
	Normal    []openAPICoverage `json:"normal"`
	Bootstrap []openAPICoverage `json:"bootstrap"`
}

var openAPICommon = map[string]bool{
	"GET /health":                   true,
	"GET /setup/status":             true,
	"POST /api/event_logging/batch": true,
}

func openAPIKeyOf(r openAPIRoute) string { return r.Method + " " + r.Path }
func openAPISymbol(f gin.HandlerFunc) string {
	return runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
}

func openAPIRouter(t *testing.T, bootstrap bool, executeCommon bool) (*gin.Engine, map[string]openAPIRoute) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	observed := map[string]openAPIRoute{}
	r.Use(func(c *gin.Context) {
		key := c.Request.Method + " " + c.FullPath()
		if executeCommon && openAPICommon[key] {
			c.Next()
			return
		}
		entry := openAPIRoute{Method: c.Request.Method, Path: c.FullPath(), Authentication: "handler-policy-unreviewed"}
		authChain := []string{}
		for _, name := range c.HandlerNames() {
			switch name {
			case openAPISymbol(openAPIJWT):
				authChain = append(authChain, "jwt")
			case openAPISymbol(openAPIOptionalJWT):
				authChain = append(authChain, "optional-jwt")
			case openAPISymbol(openAPIAdmin):
				authChain = append(authChain, "admin-jwt-or-key")
			case openAPISymbol(openAPIKey):
				authChain = append(authChain, "gateway-key")
			case openAPISymbol(openAPIStepUp):
				entry.StepUp = true
			}
			// Google auth and setup guards are constructed inside production registrars,
			// so they are identified from the actual chain rather than injected seams.
			if strings.Contains(name, "/middleware.APIKeyAuthWithSubscriptionGoogle.") {
				authChain = append(authChain, "google-gateway-key")
			}
			if strings.Contains(name, "/setup.RegisterRoutes.setupGuard.") || strings.Contains(name, "/setup.setupGuard.") {
				authChain = append(authChain, "setup-state-guard")
			}
		}
		// Preserve every recognized base-auth seam in registration order, including
		// duplicates. A later seam must never erase a preceding requirement.
		if len(authChain) > 0 {
			entry.Authentication = strings.Join(authChain, " -> ")
		}
		if !bootstrap && openAPICommon[key] && entry.Authentication == "handler-policy-unreviewed" {
			entry.Authentication = "public"
		}
		observed[key] = entry
		c.AbortWithStatus(http.StatusNoContent)
	})
	if bootstrap {
		setup.RegisterRoutes(r)
		return r, observed
	}
	cfg := &config.Config{}
	cfg.Gateway.MaxBodySize = 1024 * 1024
	cfg.Gateway.TextMaxBodySize = 1024 * 1024
	cfg.Pricing.DataDir = t.TempDir()
	h := &handler.Handlers{}
	// Zero-valued handler receivers are sufficient to REGISTER method values.
	// Recursion stops at this explicit aggregate; services are never constructed.
	fill := func(v reflect.Value) {
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.Kind() != reflect.Pointer || f.Type().Elem().Kind() != reflect.Struct {
				t.Fatalf("unexpected handler aggregate field %s", v.Type().Field(i).Name)
			}
			f.Set(reflect.New(f.Type().Elem()))
		}
	}
	fill(reflect.ValueOf(h).Elem())
	fill(reflect.ValueOf(h.Admin).Elem())
	settings := service.NewSettingService(openAPISettings{}, cfg)
	server.SetupRouter(r, h, middleware.JWTAuthMiddleware(openAPIJWT), middleware.OptionalJWTAuthMiddleware(openAPIOptionalJWT), middleware.AdminAuthMiddleware(openAPIAdmin), middleware.APIKeyAuthMiddleware(openAPIKey), middleware.AuditLogMiddleware(openAPIAudit), middleware.StepUpAuthMiddleware(openAPIStepUp), nil, nil, nil, settings, nil, cfg, nil)
	t.Cleanup(func() { middleware.SetIngressRejectRecorder(nil) })
	return r, observed
}

func openAPICollect(t *testing.T, bootstrap bool) []openAPIRoute {
	t.Helper()
	r, observed := openAPIRouter(t, bootstrap, false)
	for _, route := range r.Routes() {
		parts := strings.Split(route.Path, "/")
		for i, p := range parts {
			if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
				parts[i] = "openapi-contract-probe"
			}
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(route.Method, strings.Join(parts, "/"), nil))
		key := route.Method + " " + route.Path
		if _, ok := observed[key]; !ok {
			t.Fatalf("probe did not match registered route %s (status %d); never infer a shadowed route", key, rec.Code)
		}
	}
	actual := make([]openAPIRoute, 0, len(observed))
	for _, entry := range observed {
		actual = append(actual, entry)
	}
	sort.Slice(actual, func(i, j int) bool { return openAPIKeyOf(actual[i]) < openAPIKeyOf(actual[j]) })
	if len(actual) != len(r.Routes()) {
		t.Fatalf("inventory lost routes: observed %d registered %d", len(actual), len(r.Routes()))
	}
	return actual
}
func openAPIFile(name string) string { return filepath.Join("..", "..", "..", "api", "openapi", name) }
func openAPIRead(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile(openAPIFile(name))
	if err != nil {
		t.Fatal(err)
	}
	if err := openAPIDecode(data, v); err != nil {
		t.Fatal(err)
	}
}

// Both schema constants and response values must retain exact JSON numbers.
func openAPIDecode(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON content: %v", err)
	}
	return nil
}
func openAPICompare(actual []openAPIRoute, accepted []openAPICoverage) error {
	remaining := map[string]openAPICoverage{}
	for _, entry := range accepted {
		key := openAPIKeyOf(entry.openAPIRoute)
		if _, ok := remaining[key]; ok {
			return fmt.Errorf("duplicate accepted route: %s", key)
		}
		if entry.Owner != "api-contract" || strings.TrimSpace(entry.Reason) == "" {
			return fmt.Errorf("missing accountable coverage disposition: %s", key)
		}
		if entry.Coverage != "covered" && entry.Coverage != "uncovered" {
			return fmt.Errorf("invalid coverage: %s", key)
		}
		remaining[key] = entry
	}
	seen := map[string]bool{}
	for _, entry := range actual {
		key := openAPIKeyOf(entry)
		if seen[key] {
			return fmt.Errorf("duplicate actual route: %s", key)
		}
		seen[key] = true
		old, ok := remaining[key]
		if !ok {
			return fmt.Errorf("unaccepted route added: %s", key)
		}
		if old.openAPIRoute != entry {
			return fmt.Errorf("authentication/step-up drift: %s: accepted %+v actual %+v", key, old.openAPIRoute, entry)
		}
		delete(remaining, key)
	}
	if len(remaining) > 0 {
		keys := make([]string, 0, len(remaining))
		for k := range remaining {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return fmt.Errorf("accepted routes removed: %s", strings.Join(keys, ", "))
	}
	return nil
}
func TestOpenAPIInventory(t *testing.T) {
	var accepted openAPIInventory
	openAPIRead(t, "route-inventory.json", &accepted)
	if accepted.Format != 1 || accepted.Baseline == "" {
		t.Fatal("invalid inventory metadata")
	}
	minimum := make(map[string]bool)
	for _, entry := range accepted.Normal {
		key := openAPIKeyOf(entry.openAPIRoute)
		if openAPICommon[key] {
			if entry.Coverage != "covered" {
				t.Fatalf("minimum A0 operation cannot be demoted: %s", key)
			}
			minimum[key] = true
		}
	}
	if len(minimum) != len(openAPICommon) {
		t.Fatal("minimum A0 operations missing from inventory")
	}
	for _, scope := range []struct {
		name      string
		bootstrap bool
		accepted  []openAPICoverage
	}{{"normal", false, accepted.Normal}, {"bootstrap", true, accepted.Bootstrap}} {
		t.Run(scope.name, func(t *testing.T) {
			actual := openAPICollect(t, scope.bootstrap)
			if err := openAPICompare(actual, scope.accepted); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d actual registered method/path/auth entries matched", scope.name, len(actual))
		})
	}
}

// Observation is explicit and writes only to the caller's evidence directory.
// No test or runner silently accepts drift or rewrites committed contracts.
func TestOpenAPIObserve(t *testing.T) {
	dir := os.Getenv("OPENAPI_OBSERVED_DIR")
	if dir == "" {
		t.Skip("set OPENAPI_OBSERVED_DIR to export an unaccepted observation")
	}
	if !filepath.IsAbs(dir) {
		t.Fatal("observation output must be an absolute evidence directory")
	}
	out := map[string][]openAPIRoute{"normal": openAPICollect(t, false), "bootstrap": openAPICollect(t, true)}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "observed-routes.json")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Write(append(data, '\n')); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			t.Fatalf("write observation: %v; close observation: %v", err, closeErr)
		}
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("unaccepted observation: normal=%d bootstrap=%d path=%s", len(out["normal"]), len(out["bootstrap"]), path)
}
