package pulsarsoak

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFillBrief(t *testing.T) {
	start := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	end := start.Add(24*time.Hour + 1500*time.Millisecond)
	template := "@RUDDER_ASSIST@ assess this.\nWindow: `@WINDOW_START@` to `@WINDOW_END@`, again @WINDOW_START@." +
		"\nPing @RUDDER_ASSIST@ again.\n\n\n"

	brief, err := FillBrief(template, "U0ASSIST", start, end)
	require.NoError(t, err, "filling in")
	require.Equal(t, "<@U0ASSIST> assess this.\nWindow: `2026-10-13T12:00:00Z` to `2026-10-14T12:00:01Z`, "+
		"again 2026-10-13T12:00:00Z.\nPing <@U0ASSIST> again.", brief,
		"every placeholder is filled, the times are UTC to the second and trailing newlines are gone")
}

func TestFillBriefConvertsToUTC(t *testing.T) {
	rome := time.FixedZone("CEST", 2*60*60)
	brief, err := FillBrief("@RUDDER_ASSIST@ @WINDOW_START@ @WINDOW_END@", "U1",
		time.Date(2026, 10, 13, 14, 0, 0, 0, rome), time.Date(2026, 10, 13, 15, 0, 0, 0, rome))
	require.NoError(t, err, "filling in")
	require.Equal(t, "<@U1> 2026-10-13T12:00:00Z 2026-10-13T13:00:00Z", brief, "times in UTC")
}

func TestFillBriefFailures(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		template string
		wantErr  string
	}{
		{name: "unknown placeholder", template: "@RUDDER_ASSIST@ @WINDOW_LENGTH@ @OTHER@ @OTHER@",
			wantErr: "doesn't fill: @OTHER@ @WINDOW_LENGTH@"},
		{name: "no mention", template: "assess @WINDOW_START@", wantErr: "doesn't mention @RUDDER_ASSIST@"},
		{name: "not UTF-8", template: "@RUDDER_ASSIST@ \xff", wantErr: "UTF-8"},
		{name: "too long", template: "@RUDDER_ASSIST@ " + strings.Repeat("x", BriefLimit),
			wantErr: "bytes once filled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FillBrief(tt.template, "U1", now, now)
			require.ErrorContains(t, err, tt.wantErr, "the template is rejected")
		})
	}
}

func TestFillBriefLimitCountsBytes(t *testing.T) {
	now := time.Now()
	mention := len("<@U1> ")
	fits := "@RUDDER_ASSIST@ " + strings.Repeat("x", BriefLimit-mention)
	brief, err := FillBrief(fits, "U1", now, now)
	require.NoError(t, err, "exactly the limit fits")
	require.Len(t, brief, BriefLimit, "the brief is at the limit")

	// "é" is two bytes: 2,000 of them and the mention are 2,000 characters but over 4,000 bytes.
	_, err = FillBrief("@RUDDER_ASSIST@ "+strings.Repeat("é", BriefLimit/2), "U1", now, now)
	require.Error(t, err, "bytes count, not characters")
}

func TestCheckBrief(t *testing.T) {
	require.NoError(t, CheckBrief("@RUDDER_ASSIST@ from @WINDOW_START@ to @WINDOW_END@", "U1"), "a good template")
	require.Error(t, CheckBrief("no mention", "U1"), "a broken template")

	// A template that fits on its own but not with the dry-run marker fails the check: a dry run could use it.
	tight := "@RUDDER_ASSIST@ " + strings.Repeat("x", BriefLimit-len("<@U1> ")-len(dryRunMarker)+1)
	_, err := FillBrief(tight, "U1", time.Now(), time.Now())
	require.NoError(t, err, "the brief fits without the marker")
	require.ErrorContains(t, CheckBrief(tight, "U1"), "bytes once filled", "but not with it")

	// The sample times are as long as real ones, so the check measures what a real brief will be.
	n := BriefLimit - len("<@U1> ") - len(dryRunMarker) - 2*len("2026-10-13T12:00:00Z")
	exact := "@RUDDER_ASSIST@ " + strings.Repeat("x", n) + "@WINDOW_START@@WINDOW_END@"
	require.NoError(t, CheckBrief(exact, "U1"), "a template exactly at the limit passes")
	require.Error(t, CheckBrief(exact+"x", "U1"), "one byte more fails")
}
