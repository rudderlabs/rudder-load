package producer

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
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
}

func NewHTTPProducer(environ []string) (*HTTPProducer, error) {
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

	return &HTTPProducer{
		c:           client,
		endpoint:    endpoint,
		contentType: contentType,
		keyHeader:   keyHeader,
		clientType:  clientType,
		compression: compression,
	}, nil
}

func (p *HTTPProducer) PublishTo(_ context.Context, key string, message []byte, extra map[string]string) (int, error) {
	req := fasthttp.AcquireRequest()
	req.SetRequestURI(p.endpoint)

	fmt.Println("MESSAGE: ", string(message))
	time.Sleep(time.Hour)

	if p.compression {
		_, err := fasthttp.WriteGzipLevel(req.BodyWriter(), message, fasthttp.CompressBestSpeed)
		if err != nil {
			return 0, fmt.Errorf("cannot compress message: %w", err)
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

	res := fasthttp.AcquireResponse()
	err := p.c.Do(req, res)
	n := len(req.Body())
	fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(res)

	if err != nil {
		return 0, fmt.Errorf("http request failed: %w", err)
	}
	if res.StatusCode() != http.StatusOK {
		return 0, fmt.Errorf("http request failed with status code: %d: %s", res.StatusCode(), res.Body())
	}

	return n, err
}

func (p *HTTPProducer) Close() error {
	p.c.CloseIdleConnections()
	return nil
}
