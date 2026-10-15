package pulsarsoak

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// BriefLimit is the most bytes a brief may have. Slack splits text over 4,000 characters into several messages,
	// and rudder-assist only reads the one that mentions it; a character is at least a byte, so counting bytes stays
	// under the limit whichever Slack counts.
	BriefLimit = 4000

	placeholderAssist = "@RUDDER_ASSIST@"
	placeholderStart  = "@WINDOW_START@"
	placeholderEnd    = "@WINDOW_END@"

	// WindowLayout formats the run window as the brief's hand-off smoke test does, e.g. 2026-10-13T12:00:00Z.
	WindowLayout = "2006-01-02T15:04:05Z"
)

var placeholderRE = regexp.MustCompile(`@[A-Z_]+@`)

// FormatWindowTime formats a run window bound in UTC, to the second.
func FormatWindowTime(t time.Time) string { return t.UTC().Format(WindowLayout) }

// FillBrief fills in the brief template the way the brief's hand-off smoke test does: every @RUDDER_ASSIST@
// becomes a mention of the user, @WINDOW_START@ and @WINDOW_END@ the window bounds, and trailing newlines are dropped.
// It fails if a placeholder is left, if the brief doesn't mention rudder-assist, or if it is over BriefLimit bytes.
func FillBrief(template, assistUserID string, start, end time.Time) (string, error) {
	if !utf8.ValidString(template) {
		return "", errors.New("the brief template isn't valid UTF-8")
	}
	if !strings.Contains(template, placeholderAssist) {
		return "", fmt.Errorf("the brief template doesn't mention %s", placeholderAssist)
	}
	brief := strings.NewReplacer(
		placeholderAssist, "<@"+assistUserID+">",
		placeholderStart, FormatWindowTime(start),
		placeholderEnd, FormatWindowTime(end),
	).Replace(template)
	brief = strings.TrimRight(brief, "\n")
	if left := placeholderRE.FindAllString(brief, -1); len(left) > 0 {
		slices.Sort(left)
		return "", fmt.Errorf("the brief template has placeholders the Pulsar soak test doesn't fill: %s",
			strings.Join(slices.Compact(left), " "))
	}
	if err := checkBriefSize(brief); err != nil {
		return "", err
	}
	return brief, nil
}

// checkBriefSize fails if the brief, as it would be posted, is over BriefLimit bytes.
func checkBriefSize(brief string) error {
	if n := len(brief); n > BriefLimit {
		return fmt.Errorf("the brief is %d bytes once filled in; Slack splits text over %d characters",
			n, BriefLimit)
	}
	return nil
}

// CheckBrief fills the template with sample times and returns why a brief from it couldn't be posted, if it
// couldn't. The times have the same length as real ones, and the check counts the dry-run marker, so a template that
// passes always gives a brief within BriefLimit.
func CheckBrief(template, assistUserID string) error {
	sample := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	brief, err := FillBrief(template, assistUserID, sample, sample.Add(time.Hour))
	if err != nil {
		return err
	}
	return checkBriefSize(dryRunMarker + brief)
}
