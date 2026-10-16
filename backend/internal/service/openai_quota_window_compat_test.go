package service

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexQuotaWindowRawExplicitUnknownDuration(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, knownField := range []string{"primary", "secondary"} {
		for _, knownMinutes := range []int{300, 10080} {
			for _, unknownMinutes := range []any{0, -1, "invalid", 1440} {
				t.Run(fmt.Sprintf("%s_%d_unknown_%v", knownField, knownMinutes, unknownMinutes), func(t *testing.T) {
					unknownField := "secondary"
					if knownField == "secondary" {
						unknownField = "primary"
					}
					extra := map[string]any{
						"codex_" + knownField + "_used_percent":     25.0,
						"codex_" + knownField + "_window_minutes":   knownMinutes,
						"codex_" + knownField + "_reset_at":         now.Add(time.Hour).Format(time.RFC3339),
						"codex_" + unknownField + "_used_percent":   96.0,
						"codex_" + unknownField + "_window_minutes": unknownMinutes,
						"codex_" + unknownField + "_reset_at":       now.Add(time.Hour).Format(time.RFC3339),
						"codex_usage_updated_at":                    now.Format(time.RFC3339),
					}
					want := openAIQuotaHeadroomNeutralFactor
					if knownMinutes == 10080 {
						want = 0.75
					}
					require.InDelta(t, want, openAIQuotaHeadroomFactor(&Account{Extra: extra}, now), 1e-9)
				})
			}
		}
	}
}

func TestCodexQuotaWindowPartialDurationCompatibility(t *testing.T) {
	short, week, image, zero := 300, 10080, 1440, 0
	primaryUsed, secondaryUsed := 96.0, 25.0
	primaryReset, secondaryReset := 600, 3600
	for _, tc := range []struct {
		name             string
		primaryMinutes   *int
		secondaryMinutes *int
		shortSource      string
		weekSource       string
	}{
		{"primary_short_missing_secondary", &short, nil, "primary", "secondary"},
		{"primary_week_missing_secondary", &week, nil, "secondary", "primary"},
		{"secondary_short_missing_primary", nil, &short, "secondary", "primary"},
		{"secondary_week_missing_primary", nil, &week, "primary", "secondary"},
		{"both_missing_legacy", nil, nil, "secondary", "primary"},
		{"primary_short_secondary_image", &short, &image, "primary", ""},
		{"primary_image_secondary_short", &image, &short, "secondary", ""},
		{"primary_week_secondary_image", &week, &image, "", "primary"},
		{"primary_image_secondary_week", &image, &week, "", "secondary"},
		{"primary_image_missing_secondary", &image, nil, "", ""},
		{"secondary_image_missing_primary", nil, &image, "", ""},
		{"explicit_zero_is_not_missing", &short, &zero, "primary", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := &OpenAICodexUsageSnapshot{
				PrimaryUsedPercent: &primaryUsed, SecondaryUsedPercent: &secondaryUsed,
				PrimaryResetAfterSeconds: &primaryReset, SecondaryResetAfterSeconds: &secondaryReset,
				PrimaryWindowMinutes: tc.primaryMinutes, SecondaryWindowMinutes: tc.secondaryMinutes,
			}
			normalized := snapshot.Normalize()
			check := func(source string, used *float64, reset, minutes *int) {
				t.Helper()
				switch source {
				case "primary":
					require.Same(t, snapshot.PrimaryUsedPercent, used)
					require.Same(t, snapshot.PrimaryResetAfterSeconds, reset)
					require.Equal(t, snapshot.PrimaryWindowMinutes, minutes)
				case "secondary":
					require.Same(t, snapshot.SecondaryUsedPercent, used)
					require.Same(t, snapshot.SecondaryResetAfterSeconds, reset)
					require.Equal(t, snapshot.SecondaryWindowMinutes, minutes)
				default:
					require.Nil(t, used)
					require.Nil(t, reset)
					require.Nil(t, minutes)
				}
			}
			check(tc.shortSource, normalized.Used5hPercent, normalized.Reset5hSeconds, normalized.Window5hMinutes)
			check(tc.weekSource, normalized.Used7dPercent, normalized.Reset7dSeconds, normalized.Window7dMinutes)
		})
	}
}

func TestCodexQuotaWindowPartialDurationKeepsRawResetPairing(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, shortField := range []string{"primary", "secondary"} {
		t.Run(shortField, func(t *testing.T) {
			weekField := "secondary"
			if shortField == "secondary" {
				weekField = "primary"
			}
			extra := map[string]any{
				"codex_" + shortField + "_used_percent":   96.0,
				"codex_" + shortField + "_window_minutes": 300,
				"codex_" + shortField + "_reset_at":       now.Add(-time.Minute).Format(time.RFC3339),
				"codex_" + weekField + "_used_percent":    25.0,
				"codex_" + weekField + "_reset_at":        now.Add(time.Hour).Format(time.RFC3339),
				"codex_usage_updated_at":                  now.Format(time.RFC3339),
			}
			require.InDelta(t, 0.75, openAIQuotaHeadroomFactor(&Account{Extra: extra}, now), 1e-9)
			extra["codex_"+shortField+"_reset_at"] = now.Add(time.Hour).Format(time.RFC3339)
			require.InDelta(t, 0.375, openAIQuotaHeadroomFactor(&Account{Extra: extra}, now), 1e-9)
			extra["codex_"+weekField+"_reset_at"] = now.Add(-time.Minute).Format(time.RFC3339)
			require.Equal(t, openAIQuotaHeadroomNeutralFactor, openAIQuotaHeadroomFactor(&Account{Extra: extra}, now))
		})
	}
}

func TestCodexQuotaWindowClassificationBoundaries(t *testing.T) {
	for _, tc := range []struct {
		minutes int
		kind    string
	}{
		{-1, ""}, {0, ""}, {239, ""}, {240, "5h"}, {300, "5h"}, {360, "5h"},
		{361, ""}, {1440, ""}, {8999, ""}, {9000, "7d"}, {10080, "7d"}, {12000, "7d"}, {12001, ""},
	} {
		t.Run(fmt.Sprint(tc.minutes), func(t *testing.T) {
			kind, ok := classifyCodexUsageWindow(tc.minutes)
			require.Equal(t, tc.kind, kind)
			require.Equal(t, tc.kind != "", ok)
			for _, window := range []string{"5h", "7d"} {
				// The established canonical guard treats nonpositive values as legacy.
				// Do not silently broaden this slice into historical canonical cleanup.
				if tc.minutes > 0 {
					extra := map[string]any{"codex_" + window + "_window_minutes": tc.minutes}
					require.Equal(t, tc.kind == window, codexCanonicalWindowMatches(extra, window))
				}
			}
		})
	}
}

func TestCodexQuotaWindowNilRawDurationKeepsLegacyComplement(t *testing.T) {
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, raw := range []any{nil, 0, -1, "invalid", 1440} {
		t.Run(fmt.Sprintf("%v", raw), func(t *testing.T) {
			extra := map[string]any{"codex_primary_used_percent": 25.0, "codex_primary_window_minutes": 10080,
				"codex_secondary_used_percent": 96.0, "codex_secondary_window_minutes": raw,
				"codex_usage_updated_at": now.Format(time.RFC3339)}
			short, week := openAICanonicalQuotaWindows(extra, now)
			require.True(t, week.hasUsed)
			require.Equal(t, 25.0, week.usedPercent)
			require.Equal(t, raw == nil, short.hasUsed)
			require.Equal(t, raw, extra["codex_secondary_window_minutes"], "reader must not rewrite raw evidence")
		})
	}
}

func TestCodexQuotaWindowHeaderAndBodyPrecedence(t *testing.T) {
	bodyReset := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	body := []byte(fmt.Sprintf(`{"error":{"type":"usage_limit_reached","resets_at":%d}}`, bodyReset))
	headers := http.Header{}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-window-minutes", "10080")
	headers.Set("x-codex-primary-reset-after-seconds", "7200")
	headers.Set("x-codex-secondary-used-percent", "100")
	headers.Set("x-codex-secondary-window-minutes", "300")
	headers.Set("x-codex-secondary-reset-after-seconds", "3600")
	headers.Set("x-unrecognized-window-minutes", "1440")
	before := time.Now()
	disposition, reset := classifyOpenAIOAuth429(headers, body)
	after := time.Now()
	require.Equal(t, openAIOAuth429Quota7d, disposition)
	require.NotNil(t, reset)
	require.False(t, reset.Before(before.Add(7200*time.Second)))
	require.False(t, reset.After(after.Add(7200*time.Second)))
	// A known-but-unsupported image window must not become account-wide 7d.
	headers.Del("x-codex-secondary-used-percent")
	headers.Del("x-codex-secondary-window-minutes")
	headers.Del("x-codex-secondary-reset-after-seconds")
	headers.Set("x-codex-primary-window-minutes", "1440")
	snapshot := ParseCodexRateLimitHeaders(headers)
	require.NotNil(t, snapshot.PrimaryWindowMinutes)
	require.Equal(t, 1440, *snapshot.PrimaryWindowMinutes)
	require.Nil(t, calculateOpenAI429ResetTime(headers))
	disposition, reset = classifyOpenAIOAuth429(headers, nil)
	require.Equal(t, openAIOAuth429Transient, disposition)
	require.Nil(t, reset)
	// The independent explicit body-reset signal still works; do not weaken it.
	disposition, reset = classifyOpenAIOAuth429(headers, body)
	require.Equal(t, openAIOAuth429QuotaReset, disposition)
	require.NotNil(t, reset)
	require.Equal(t, bodyReset, reset.Unix())
}
