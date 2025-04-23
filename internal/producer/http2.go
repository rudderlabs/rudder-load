package producer

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/rudderlabs/rudder-go-kit/httputil"

	"github.com/valyala/fasthttp"
	"golang.org/x/net/http2"
)

type HTTP2Producer struct {
	c           *http.Client
	endpoint    string
	contentType string
	keyHeader   string
	clientType  string
	compression bool
	slotName    string
}

func NewHTTP2Producer(slotName string, environ []string) (*HTTP2Producer, error) {
	conf, err := readConfiguration("HTTP2_", environ)
	if err != nil {
		return nil, fmt.Errorf("cannot read http configuration: %v", err)
	}
	endpoint, err := getRequiredStringSetting(conf, "endpoint")
	if err != nil {
		return nil, err
	}
	timeout, err := getOptionalDurationSetting(conf, "timeout", 600*time.Second)
	if err != nil {
		return nil, err
	}
	idleConnTimeout, err := getOptionalDurationSetting(conf, "idle_conn_timeout", 30*time.Second)
	if err != nil {
		return nil, err
	}
	compression, err := getOptionalBoolSetting(conf, "compression", false)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http2.Transport{
			IdleConnTimeout: idleConnTimeout,
			AllowHTTP:       true,
			DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
		Timeout: timeout,
	}

	contentType, err := getOptionalStringSetting(conf, "content_type", "application/json; charset=utf-8")
	if err != nil {
		return nil, err
	}
	keyHeader, err := getOptionalStringSetting(conf, "key_header", "")
	if err != nil {
		return nil, err
	}

	return &HTTP2Producer{
		c:           client,
		endpoint:    endpoint,
		contentType: contentType,
		keyHeader:   keyHeader,
		compression: compression,
		slotName:    slotName,
	}, nil
}

func (p *HTTP2Producer) PublishTo(ctx context.Context, key string, message []byte, extra map[string]string) ([]byte, error) {
	var body io.Reader = bytes.NewBuffer(message)

	if p.compression {
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		if _, err := gz.Write(message); err != nil {
			return nil, fmt.Errorf("failed to compress message: %w", err)
		}
		if err := gz.Close(); err != nil {
			return nil, fmt.Errorf("failed to finalize compression: %w", err)
		}
		body = &buf
	}

	req, err := http.NewRequestWithContext(ctx, fasthttp.MethodPost, p.endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("cannot create http request: %w", err)
	}

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

	res, err := p.c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer httputil.CloseResponse(res)

	responseBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read response body: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http request failed with status code: %d: %s", res.StatusCode, responseBody)
	}

	return responseBody, err
}

func (p *HTTP2Producer) Close() error {
	p.c.CloseIdleConnections()
	return nil
}
