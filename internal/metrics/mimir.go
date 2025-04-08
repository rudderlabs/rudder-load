package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"rudder-load/internal/parser"
)

type MimirClient interface {
	Query(ctx context.Context, query string, time int64) (QueryResponse, error)
	QueryRange(ctx context.Context, query string, start int64, end int64, step string) (QueryResponse, error)
	GetMetrics(ctx context.Context, mts []parser.Metric) ([]MetricsResponse, error)
}

type mimirClient struct {
	baseURL string
	client  *http.Client
}

type QueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
			Values [][]interface{}   `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

type MetricsResponse struct {
	Key   string
	Value float64
}

func NewMimirClient(baseURL string) MimirClient {
	return &mimirClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (m *mimirClient) Query(ctx context.Context, query string, time int64) (QueryResponse, error) {
	var queryResp QueryResponse

	reqURL := fmt.Sprintf("%s/prometheus/api/v1/query", m.baseURL)
	params := url.Values{}
	params.Add("query", query)
	params.Add("time", fmt.Sprintf("%d", time))

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL+"?"+params.Encode(), nil)
	if err != nil {
		return queryResp, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Add("X-Scope-OrgID", "allTenants")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.client.Do(req)
	if err != nil {
		return queryResp, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return queryResp, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return queryResp, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, &queryResp); err != nil {
		return queryResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if queryResp.Status != "success" {
		return queryResp, fmt.Errorf("query failed with status: %s", queryResp.Status)
	}

	return queryResp, nil
}

func (m *mimirClient) QueryRange(ctx context.Context, query string, start int64, end int64, step string) (QueryResponse, error) {
	var queryResp QueryResponse
	reqURL := fmt.Sprintf("%s/prometheus/api/v1/query_range", m.baseURL)

	req, err := http.NewRequestWithContext(ctx, "POST", reqURL, nil)
	if err != nil {
		return queryResp, fmt.Errorf("failed to create request: %w", err)
	}

	q := req.URL.Query()
	q.Add("query", query)
	q.Add("start", fmt.Sprintf("%d", start))
	q.Add("end", fmt.Sprintf("%d", end))
	q.Add("step", step)

	req.URL.RawQuery = q.Encode()

	req.Header.Add("X-Scope-OrgID", "allTenants")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.client.Do(req)
	if err != nil {
		return queryResp, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return queryResp, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return queryResp, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	if err := json.Unmarshal(body, &queryResp); err != nil {
		return queryResp, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if queryResp.Status != "success" {
		return queryResp, fmt.Errorf("query failed with status: %s", queryResp.Status)
	}

	return queryResp, nil
}

func (m *mimirClient) GetMetrics(ctx context.Context, mts []parser.Metric) ([]MetricsResponse, error) {
	var metricsResponses []MetricsResponse

	var knownMetrics = []struct {
		Name  string
		Query string
	}{
		{Name: "rps", Query: "sum(rate(rudder_load_publish_duration_seconds_count[1m]))"},
	}

	for _, metric := range mts {
		var matchingMetric *struct {
			Name  string
			Query string
		}
		for _, km := range knownMetrics {
			if km.Name == metric.Name {
				matchingMetric = &km
				break
			}
		}

		if matchingMetric != nil && metric.Query == "" {
			metric.Query = matchingMetric.Query
		}

		resp, err := m.Query(ctx, metric.Query, time.Now().Unix())

		if err != nil {
			return metricsResponses, fmt.Errorf("failed to query %s: %w", metric.Name, err)
		}

		if len(resp.Data.Result) > 0 {
			if str, ok := resp.Data.Result[0].Value[1].(string); ok {
				value, err := strconv.ParseFloat(str, 64)
				if err != nil {
					return metricsResponses, fmt.Errorf("failed to parse %s value: %w", metric.Name, err)
				}
				metricsResponses = append(metricsResponses, MetricsResponse{Key: metric.Name, Value: math.Round(value)})
			}
		}
	}
	return metricsResponses, nil
}
