package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

var outputNameRE = regexp.MustCompile(`^[a-z_]+$`)

// commandGuard turns GitHub Actions workflow commands off between stopCommands and resume, so nothing printed in
// between, such as a version label from Grafana or an error from Slack, can issue one. Only an unguessable token
// turns them back on.
type commandGuard struct {
	w     io.Writer
	token string
}

// stopCommands prints ::stop-commands:: with a random token.
func stopCommands(w io.Writer) (*commandGuard, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("making the stop-commands token: %w", err)
	}
	g := &commandGuard{w: w, token: hex.EncodeToString(b)}
	if _, err := io.WriteString(w, "::stop-commands::"+g.token+"\n"); err != nil {
		return nil, fmt.Errorf("turning workflow commands off: %w", err)
	}
	return g, nil
}

// resume turns workflow commands back on.
func (g *commandGuard) resume() error {
	if _, err := io.WriteString(g.w, "::"+g.token+"::\n"); err != nil {
		return fmt.Errorf("turning workflow commands back on: %w", err)
	}
	return nil
}

// annotate prints a workflow annotation, such as an error, with the message escaped the way the runner expects. Only
// text the Pulsar soak test wrote itself may go here: annotations are printed with workflow commands on.
func annotate(w io.Writer, level, message string) error {
	message = strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(message)
	if _, err := io.WriteString(w, "::"+level+"::"+message+"\n"); err != nil {
		return fmt.Errorf("printing an annotation: %w", err)
	}
	return nil
}

// writeOutput appends name=value to the $GITHUB_OUTPUT file at path, if there is one.
func writeOutput(path, name, value string) (err error) {
	if path == "" {
		return nil
	}
	if !outputNameRE.MatchString(name) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("the output %q can't be written safely", name)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("opening $GITHUB_OUTPUT: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("closing $GITHUB_OUTPUT: %w", cerr))
		}
	}()
	if _, err := io.WriteString(f, name+"="+value+"\n"); err != nil {
		return fmt.Errorf("writing $GITHUB_OUTPUT: %w", err)
	}
	return nil
}
