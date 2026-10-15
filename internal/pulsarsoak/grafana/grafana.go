// Package grafana queries Prometheus-compatible and Loki datasources through Grafana's datasource proxy.
package grafana

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rudderlabs/rudder-go-kit/httputil"
)

const (
	// DefaultTimeout bounds every request, including reading its body.
	DefaultTimeout = 30 * time.Second
	// DefaultAttempts is how many times a query is tried before giving up.
	DefaultAttempts = 3

	maxBodyBytes  = 16 << 20
	maxErrorBytes = 256
)

// Sample is one series of an instant vector.
type Sample struct {
	Labels map[string]string
	Value  float64
	Time   time.Time
}

// Client queries datasources through Grafana's datasource proxy with a service account token.
type Client struct {
	base     *url.URL
	token    string
	http     *http.Client
	attempts int
	backoff  func(attempt int) time.Duration
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client, e.g. one with a different timeout.
func WithHTTPClient(c *http.Client) Option { return func(cl *Client) { cl.http = c } }

// WithBackoff sets how long to wait before retry number attempt (1 for the first retry).
func WithBackoff(f func(attempt int) time.Duration) Option {
	return func(cl *Client) { cl.backoff = f }
}

// WithAttempts sets how many times a query is tried.
func WithAttempts(n int) Option { return func(cl *Client) { cl.attempts = max(n, 1) } }

// New returns a client for the Grafana at baseURL, e.g. https://example.grafana.net.
func New(baseURL, token string, opts ...Option) (*Client, error) {
	u, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parsing the Grafana URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" || u.Host == "" {
		return nil, errors.New("the Grafana URL must be an absolute http(s) URL")
	}
	c := &Client{
		base:     u,
		token:    token,
		http:     httputil.NewHttpClient(httputil.WithTimeout(DefaultTimeout)),
		attempts: DefaultAttempts,
		backoff:  func(attempt int) time.Duration { return time.Duration(attempt) * time.Second },
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Query runs an instant PromQL query at the given time against the Prometheus-compatible datasource uid.
func (c *Client) Query(ctx context.Context, uid, query string, at time.Time) ([]Sample, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("time", strconv.FormatFloat(float64(at.UnixMilli())/1000, 'f', 3, 64))
	var resp promResponse
	if err := c.get(ctx, uid, "api/v1/query", params, &resp); err != nil {
		return nil, err
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus answered %s: %s", clean(resp.Status), clean(resp.Error))
	}
	if resp.Data.ResultType != "vector" {
		return nil, fmt.Errorf("prometheus answered a %s, not a vector", clean(resp.Data.ResultType))
	}
	samples := make([]Sample, 0, len(resp.Data.Result))
	for _, r := range resp.Data.Result {
		ts, v, err := parsePoint(r.Value)
		if err != nil {
			return nil, err
		}
		samples = append(samples, Sample{Labels: r.Metric, Value: v, Time: ts})
	}
	return samples, nil
}

// LatestLog returns the time of the newest log line matching the LogQL stream selector between start and end, and
// false if there is none.
func (c *Client) LatestLog(ctx context.Context, uid, query string, start, end time.Time) (time.Time, bool, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(start.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(end.UnixNano(), 10))
	params.Set("limit", "1")
	params.Set("direction", "backward")
	var resp lokiResponse
	if err := c.get(ctx, uid, "loki/api/v1/query_range", params, &resp); err != nil {
		return time.Time{}, false, err
	}
	if resp.Status != "success" {
		return time.Time{}, false, fmt.Errorf("loki answered %s", clean(resp.Status))
	}
	if resp.Data.ResultType != "streams" {
		return time.Time{}, false, fmt.Errorf("loki answered %s, not streams", clean(resp.Data.ResultType))
	}
	var latest time.Time
	for _, s := range resp.Data.Result {
		for _, v := range s.Values {
			if len(v) == 0 {
				continue
			}
			ns, err := strconv.ParseInt(v[0], 10, 64)
			if err != nil {
				return time.Time{}, false, fmt.Errorf("parsing a log line's timestamp: %w", err)
			}
			if t := time.Unix(0, ns).UTC(); t.After(latest) {
				latest = t
			}
		}
	}
	return latest, !latest.IsZero(), nil
}

type promResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []json.RawMessage `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

type lokiResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][]string `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// parsePoint parses a Prometheus [<unix seconds>, "<value>"] pair.
func parsePoint(p []json.RawMessage) (time.Time, float64, error) {
	if len(p) != 2 {
		return time.Time{}, 0, fmt.Errorf("a sample has %d elements, not 2", len(p))
	}
	var secs float64
	if err := json.Unmarshal(p[0], &secs); err != nil {
		return time.Time{}, 0, fmt.Errorf("parsing a sample's time: %w", err)
	}
	var s string
	if err := json.Unmarshal(p[1], &s); err != nil {
		return time.Time{}, 0, fmt.Errorf("parsing a sample's value: %w", err)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parsing a sample's value: %w", err)
	}
	return SecondsToTime(secs), v, nil
}

// SecondsToTime converts fractional Unix seconds, as Prometheus returns them, to a UTC time with millisecond
// precision.
func SecondsToTime(secs float64) time.Time {
	return time.UnixMilli(int64(math.Round(secs * 1000))).UTC()
}

// get calls the datasource proxy and decodes the JSON answer into out. Transport errors, 429 and 5xx answers are
// retried; other answers are not, since repeating a bad query can't help.
func (c *Client) get(ctx context.Context, uid, path string, params url.Values, out any) error {
	u := c.base.JoinPath("api", "datasources", "proxy", "uid", url.PathEscape(uid), path)
	u.RawQuery = params.Encode()
	var err error
	for attempt := 0; attempt < c.attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errors.Join(err, ctx.Err())
			case <-time.After(c.backoff(attempt)):
			}
		}
		var retry bool
		retry, err = c.do(ctx, u.String(), out)
		if err == nil || !retry {
			return err
		}
	}
	return err
}

func (c *Client) do(ctx context.Context, u string, out any) (retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, fmt.Errorf("building the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return ctx.Err() == nil, fmt.Errorf("querying Grafana: %w", err)
	}
	defer func() { httputil.CloseResponse(resp) }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return ctx.Err() == nil, fmt.Errorf("reading Grafana's answer: %w", err)
	}
	if len(body) > maxBodyBytes {
		return false, fmt.Errorf("grafana's answer is over %d bytes", maxBodyBytes)
	}
	if resp.StatusCode != http.StatusOK {
		retry = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return retry, fmt.Errorf("grafana answered %d: %s", resp.StatusCode, clean(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("decoding Grafana's answer: %w", err)
	}
	return false, nil
}

// clean shortens text from a response and drops control characters, so an error message stays on one line.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) > maxErrorBytes {
		cut := maxErrorBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return strings.TrimSpace(s)
}
