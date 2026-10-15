package pulsarsoak

import (
	"strconv"
	"strings"
	"time"
)

const dryRunMarker = "🧪 Dry run: "

var slackEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// esc escapes text for Slack, so a value such as a version label can't add mentions or links.
func esc(s string) string { return slackEscaper.Replace(s) }

// code formats a value as inline code.
func code(s string) string { return "`" + esc(s) + "`" }

// slackTime shows t in each reader's time zone, falling back to UTC.
func slackTime(t time.Time) string {
	return "<!date^" + strconv.FormatInt(t.Unix(), 10) + "^{date_short_pretty} {time}|" + FormatWindowTime(t) + ">"
}

func marker(r *Run) string {
	if r.DryRun {
		return dryRunMarker
	}
	return ""
}

func announceText(cfg Config, r *Run) string {
	var b strings.Builder
	b.WriteString(marker(r))
	b.WriteString("🔍 New Pulsar version detected: " + code(r.FromVersion) + " → " + code(r.Version) + "\n")
	b.WriteString("Pulsar soak test " + code(r.ID) + " in " + code(cfg.Namespace) +
		": starts " + slackTime(r.DetectedAt) +
		", expected end " + slackTime(r.DetectedAt.Add(time.Duration(r.Length))))
	if cfg.RunURL != "" {
		b.WriteString(" · <" + cfg.RunURL + "|workflow run>")
	}
	return b.String()
}

func blockedLoadText(cfg Config) string {
	return "blocked: another load test is running in " + esc(cfg.Namespace)
}

func blockedDiscardText(cfg Config) string {
	return "blocked: src-router discard is not true in " + esc(cfg.Namespace)
}

func startedText(r *Run) string {
	return "▶️ started at " + slackTime(r.StartedAt) + "; the load stops at " + slackTime(r.Deadline)
}

func abortedText(r *Run) string {
	if r.EndReason == EndUnverified {
		return "🛑 aborted: src-router discard could not be verified for " + unverifiedMax.String()
	}
	return "🛑 aborted: src-router discard is no longer true"
}

func supersededText(cfg Config, version, newThreadTS string) string {
	text := "superseded by " + code(version)
	if link := threadLink(cfg, newThreadTS); link != "" {
		return text + ": <" + link + "|new thread>"
	}
	return text + ", announced in a new thread"
}

func tagText(cfg Config, r *Run) string {
	return "<!subteam^" + cfg.ServerEngGroupID + "> the Pulsar soak test of " + code(r.Version) +
		" is done, and it's yours from here."
}

// threadLink returns a link to the message ts, or "" without a workspace URL or a ts.
func threadLink(cfg Config, ts string) string {
	if cfg.SlackWorkspaceURL == "" || ts == "" {
		return ""
	}
	return strings.TrimSuffix(cfg.SlackWorkspaceURL, "/") + "/archives/" + cfg.SlackChannel + "/p" +
		strings.Replace(ts, ".", "", 1)
}

// reply returns the text and thread of a reply in r's thread. If the announcement's ts isn't known, the reply goes
// to the channel instead, naming the run when withContext is set, since it won't be next to the announcement.
func reply(r *Run, text string, withContext bool) (string, string) {
	text = marker(r) + text
	if ts := r.ThreadTS(); ts != "" {
		return text, ts
	}
	if withContext {
		text = "Pulsar soak test " + code(r.ID) + ": " + text
	}
	return text, ""
}
