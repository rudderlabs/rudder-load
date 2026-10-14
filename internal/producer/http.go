package producer

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/valyala/fasthttp"
)

const (
	clientTypeFastHTTP = "fasthttp"
	clientTypeHTTP     = "http"
)

type HTTPProducer struct {
	c           *fasthttp.Client
	endpoint    string
	contentType string
	keyHeader   string
	clientType  string
	compression bool
	slotName    string
	gzipPool    sync.Pool
}

func NewHTTPProducer(slotName string, environ []string) (*HTTPProducer, error) {
	conf, err := readConfiguration("HTTP_", environ)
	if err != nil {
		return nil, fmt.Errorf("cannot read http configuration: %v", err)
	}
	clientType, err := getOptionalStringSetting(conf, "client_type", clientTypeHTTP)
	if err != nil {
		return nil, err
	}
	if clientType != clientTypeHTTP && clientType != clientTypeFastHTTP {
		return nil, fmt.Errorf("client type out of the known domain [%s,%s]: %s", clientTypeHTTP, clientTypeFastHTTP, clientType)
	}
	endpoint, err := getRequiredStringSetting(conf, "endpoint")
	if err != nil {
		return nil, err
	}
	_, err = url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %v", err)
	}
	readTimeout, err := getOptionalDurationSetting(conf, "read_timeout", 500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	writeTimeout, err := getOptionalDurationSetting(conf, "write_timeout", 500*time.Millisecond)
	if err != nil {
		return nil, err
	}
	maxIdleConn, err := getOptionalDurationSetting(conf, "max_idle_conn", time.Hour)
	if err != nil {
		return nil, err
	}
	maxConnsPerHost, err := getOptionalIntSetting(conf, "max_conns_per_host", 5000)
	if err != nil {
		return nil, err
	}
	concurrency, err := getOptionalIntSetting(conf, "concurrency", 1000)
	if err != nil {
		return nil, err
	}
	compression, err := getOptionalBoolSetting(conf, "compression", false)
	if err != nil {
		return nil, err
	}

	client := &fasthttp.Client{
		ReadTimeout:                   readTimeout,
		WriteTimeout:                  writeTimeout,
		MaxIdleConnDuration:           maxIdleConn,
		NoDefaultUserAgentHeader:      true, // Don't send: User-Agent: fasthttp
		DisableHeaderNamesNormalizing: true, // If you set the case on your headers correctly you can enable this
		DisablePathNormalizing:        true,
		// increase DNS cache time to an hour instead of default minute
		MaxConnsPerHost: int(maxConnsPerHost),
		Dial: (&fasthttp.TCPDialer{
			Concurrency:      int(concurrency),
			DNSCacheDuration: time.Hour,
		}).Dial,
	}

	contentType, err := getOptionalStringSetting(conf, "content_type", "text/plain; charset=utf-8")
	if err != nil {
		return nil, err
	}
	keyHeader, err := getOptionalStringSetting(conf, "key_header", "")
	if err != nil {
		return nil, err
	}

	p := &HTTPProducer{
		c:           client,
		endpoint:    endpoint,
		contentType: contentType,
		keyHeader:   keyHeader,
		clientType:  clientType,
		compression: compression,
		slotName:    slotName,
	}
	if compression {
		p.gzipPool = sync.Pool{
			New: func() any {
				w, err := gzip.NewWriterLevel(nil, gzip.BestSpeed)
				if err != nil { // unreachable: BestSpeed is a valid level
					panic(fmt.Errorf("creating gzip writer: %w", err))
				}
				return w
			},
		}
	}
	return p, nil
}

func (p *HTTPProducer) PublishTo(_ context.Context, key string, message []byte, extra map[string]string) ([]byte, error) {
	req := fasthttp.AcquireRequest()
	req.SetRequestURI(p.endpoint)

	if p.compression {
		gz := p.gzipPool.Get().(*gzip.Writer)
		gz.Reset(req.BodyWriter())
		_, err := gz.Write(message)
		err = errors.Join(err, gz.Close())
		p.gzipPool.Put(gz)
		if err != nil {
			fasthttp.ReleaseRequest(req)
			return nil, fmt.Errorf("compressing message: %w", err)
		}
		req.Header.Set("Content-Encoding", "gzip")
	} else {
		req.SetBody(message)
	}

	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentType(p.contentType)
	if p.keyHeader != "" {
		req.Header.Set(p.keyHeader, key)
	}
	if auth, ok := extra["auth"]; ok {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(auth+":")))
	}
	if anonymousID, ok := extra["anonymous_id"]; ok {
		req.Header.Set("AnonymousId", anonymousID)
	}

	// Set X-SlotName header
	if p.slotName != "" {
		req.Header.Set("X-SlotName", p.slotName)
	}

	res := fasthttp.AcquireResponse()
	err := p.c.Do(req, res)
	fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(res)

	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	if res.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("http request failed with status code: %d: %s", res.StatusCode(), res.Body())
	}

	// The body lives in res, which goes back to fasthttp's pool when this returns, so it has to be copied.
	body := make([]byte, len(res.Body()))
	copy(body, res.Body())
	return body, nil
}

func (p *HTTPProducer) Close() error {
	p.c.CloseIdleConnections()
	return nil
}

// RetryReason returns why a failed HTTP request is worth sending again, as a real client would, or "" if it isn't:
//   - "dns": a DNS lookup failed, other than with "no such host".
//   - "dial": the connection couldn't be opened (dial timeout, connection refused).
//   - "write": the connection broke while the request was being sent. A server that reads the whole body before
//     acting on it, as ingestion does, can't have processed it.
//   - "connection_closed": the server closed the connection before sending any response byte. A server does that to
//     idle keep-alive connections while shutting down, but also when it processed the request and went away before
//     answering, so this retry can send a request twice.
//
// Other errors, like a timeout or a non-200 status, aren't retried: the server may have processed the request.
func RetryReason(err error) string {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return ""
		}
		return "dns"
	case errors.Is(err, fasthttp.ErrDialTimeout), errors.As(err, &opErr) && opErr.Op == "dial":
		return "dial"
	case errors.As(err, &opErr) && opErr.Op == "write" && !opErr.Timeout():
		return "write"
	case errors.Is(err, fasthttp.ErrConnectionClosed):
		return "connection_closed"
	default:
		return ""
	}
}

// IsRetryable reports whether a failed HTTP request is worth sending again (see RetryReason).
func IsRetryable(err error) bool {
	return RetryReason(err) != ""
}
