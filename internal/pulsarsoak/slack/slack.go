// Package slack posts messages with Slack's chat.postMessage, the only method the Pulsar soak test's bot may call.
package slack

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/rudderlabs/rudder-go-kit/httputil"
)

const (
	// DefaultEndpoint is chat.postMessage.
	DefaultEndpoint = "https://slack.com/api/chat.postMessage"
	// DefaultTimeout bounds a post, including reading Slack's answer.
	DefaultTimeout = 30 * time.Second

	maxBodyBytes = 1 << 20
)

// ErrNotPosted marks a failure after which Slack certainly hasn't posted the message, so posting it again later
// can't show it twice. Any other error leaves it unknown whether the message was posted.
var ErrNotPosted = errors.New("slack did not post the message")

var (
	tsRE        = regexp.MustCompile(`^[0-9]{1,20}\.[0-9]{1,10}$`)
	errorCodeRE = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
)

// ValidTS reports whether s looks like a Slack message timestamp, such as 1792072700.123456.
func ValidTS(s string) bool { return tsRE.MatchString(s) }

// Client posts to one channel with a bot token.
type Client struct {
	token    string
	channel  string
	endpoint string
	http     *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithEndpoint sets the chat.postMessage URL, for tests.
func WithEndpoint(u string) Option { return func(c *Client) { c.endpoint = u } }

// WithHTTPClient sets the HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// New returns a client that posts to channel with the bot token.
func New(token, channel string, opts ...Option) *Client {
	c := &Client{
		token:    token,
		channel:  channel,
		endpoint: DefaultEndpoint,
		http:     httputil.NewHttpClient(httputil.WithTimeout(DefaultTimeout)),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type postRequest struct {
	Channel     string `json:"channel"`
	Text        string `json:"text"`
	ThreadTS    string `json:"thread_ts,omitempty"`
	UnfurlLinks bool   `json:"unfurl_links"`
	UnfurlMedia bool   `json:"unfurl_media"`
}

type postResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	TS    string `json:"ts"`
}

// PostMessage posts text to the channel, as a reply in the thread threadTS unless it's empty, and returns the new
// message's timestamp. It is never retried: a retry after a timeout could post the message twice. An error wrapping
// ErrNotPosted means the message certainly wasn't posted. A successful post may return an empty timestamp if Slack's
// answer has none that looks valid.
func (c *Client) PostMessage(ctx context.Context, text, threadTS string) (string, error) {
	if threadTS != "" && !ValidTS(threadTS) {
		return "", fmt.Errorf("%w: the thread timestamp is not valid", ErrNotPosted)
	}
	body, err := json.Marshal(postRequest{Channel: c.channel, Text: text, ThreadTS: threadTS})
	if err != nil {
		return "", fmt.Errorf("%w: encoding the message: %w", ErrNotPosted, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%w: building the request: %w", ErrNotPosted, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotPosted, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if notSent(err) {
			return "", fmt.Errorf("%w: connecting to Slack: %w", ErrNotPosted, err)
		}
		return "", fmt.Errorf("posting to Slack, outcome unknown: %w", err)
	}
	defer func() { httputil.CloseResponse(resp) }()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", fmt.Errorf("%w: slack rate-limited the post", ErrNotPosted)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return "", fmt.Errorf("%w: slack answered %d", ErrNotPosted, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("posting to Slack, outcome unknown: slack answered %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", fmt.Errorf("reading Slack's answer, outcome unknown: %w", err)
	}
	var pr postResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return "", fmt.Errorf("decoding Slack's answer, outcome unknown: %w", err)
	}
	if !pr.OK {
		code := pr.Error
		if !errorCodeRE.MatchString(code) {
			code = "unrecognised error"
		}
		return "", fmt.Errorf("%w: slack answered %s", ErrNotPosted, code)
	}
	if !ValidTS(pr.TS) {
		return "", nil
	}
	return pr.TS, nil
}

// notSent reports whether a transport error happened before the request could reach Slack.
func notSent(err error) bool {
	var (
		opErr   *net.OpError
		dnsErr  *net.DNSError
		certErr *tls.CertificateVerificationError
		authErr x509.UnknownAuthorityError
		hostErr x509.HostnameError
	)
	switch {
	case errors.As(err, &dnsErr), errors.As(err, &certErr), errors.As(err, &authErr), errors.As(err, &hostErr):
		return true
	case errors.As(err, &opErr):
		return opErr.Op == "dial"
	}
	return false
}
