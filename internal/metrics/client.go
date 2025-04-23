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
	"strings"
	"time"

	"rudder-load/internal/parser"
)

type MetricsFetcher struct {
	baseURL string
	client  *http.Client
	isLocal bool
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

func NewMetricsFetcher(baseURL string) *MetricsFetcher {
	return &MetricsFetcher{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		isLocal: false,
	}
}

func NewLocalMetricsFetcher(baseURL string) *MetricsFetcher {
	return &MetricsFetcher{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		isLocal: true,
	}
}

func (m *MetricsFetcher) Query(ctx context.Context, query string, time int64) (QueryResponse, error) {
	if m.isLocal {
		return QueryResponse{}, fmt.Errorf("Query method not supported for local metrics fetcher")
	}
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

func (m *MetricsFetcher) QueryRange(ctx context.Context, query string, start int64, end int64, step string) (QueryResponse, error) {
	if m.isLocal {
		return QueryResponse{}, fmt.Errorf("QueryRange method not supported for local metrics fetcher")
	}
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

func (m *MetricsFetcher) GetMetrics(ctx context.Context, mts []parser.Metric) ([]MetricsResponse, error) {
	if m.isLocal {
		return m.getLocalMetrics(ctx, mts)
	}

	var metricsResponses []MetricsResponse
	knownMetrics := map[string]string{
		"rps": "sum(rate(rudder_load_publish_duration_seconds_count[1m]))",
	}

	for _, metric := range mts {
		if query, ok := knownMetrics[metric.Name]; ok {
			metric.Query = query
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

func (m *MetricsFetcher) getLocalMetrics(ctx context.Context, mts []parser.Metric) ([]MetricsResponse, error) {
	var metricsResponses []MetricsResponse

	req, err := http.NewRequestWithContext(ctx, "GET", m.baseURL, nil)
	if err != nil {
		return metricsResponses, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return metricsResponses, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return metricsResponses, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return metricsResponses, fmt.Errorf("unexpected status code: %d, body: %s", resp.StatusCode, string(body))
	}

	metricsText := string(body)
	metricsLines := strings.Split(metricsText, "\n")

	metricMap := make(map[string]float64)
	for _, line := range metricsLines {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			continue
		}

		// Extract metric name and labels
		metricWithLabels := parts[0]
		metricName := strings.Split(metricWithLabels, "{")[0]

		// Extract metric value
		metricValue, err := strconv.ParseFloat(parts[len(parts)-1], 64)
		if err != nil {
			return metricsResponses, fmt.Errorf("failed to parse metric value: %w", err)
		}

		metricMap[metricName] = metricValue
	}

	for _, metric := range mts {
		if value, ok := metricMap[metric.Name]; ok {
			metricsResponses = append(metricsResponses, MetricsResponse{
				Key:   metric.Name,
				Value: value,
			})
		}
	}

	return metricsResponses, nil
}
