package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
