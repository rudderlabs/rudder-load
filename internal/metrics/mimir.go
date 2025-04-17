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

type MimirClient interface {
	Query(ctx context.Context, query string, time int64) (QueryResponse, error)
	QueryRange(ctx context.Context, query string, start int64, end int64, step string) (QueryResponse, error)
	GetMetrics(ctx context.Context, mts []parser.Metric) ([]MetricsResponse, error)
}

type mimirClient struct {
	baseURL string
	client  *http.Client
}

type localMetricsClient struct {
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

func NewLocalMetricsClient(baseURL string) MimirClient {
	return &localMetricsClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (m *localMetricsClient) Query(ctx context.Context, query string, time int64) (QueryResponse, error) {
	return QueryResponse{}, fmt.Errorf("query not supported for local metrics client")
}

func (m *localMetricsClient) QueryRange(ctx context.Context, query string, start int64, end int64, step string) (QueryResponse, error) {
	return QueryResponse{}, fmt.Errorf("query_range not supported for local metrics client")
}

func (m *localMetricsClient) GetMetrics(ctx context.Context, mts []parser.Metric) ([]MetricsResponse, error) {
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

	// Parse the Prometheus format metrics
	metricsText := string(body)
	metricsLines := strings.Split(metricsText, "\n")

	// Create a map of metric names to their values
	metricMap := make(map[string]float64)
	for _, line := range metricsLines {
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		// Parse the metric line
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
			continue
		}

		// Store the metric with its full name (including labels)
		metricMap[metricName] = metricValue
	}

	// Process the requested metrics
	for _, metric := range mts {
		// For local metrics, we directly look up the metric name
		metricName := metric.Name

		// Try to find an exact match first
		var found bool
		var value float64

		// First try to find an exact match
		if val, ok := metricMap[metricName]; ok {
			value = val
			found = true
		} else {
			// If no exact match, try to find a partial match
			for key, val := range metricMap {
				if strings.HasPrefix(key, metricName) {
					value = val
					found = true
					break
				}
			}
		}

		if found {
			metricsResponses = append(metricsResponses, MetricsResponse{
				Key:   metric.Name,
				Value: math.Round(value),
			})
		}
	}

	return metricsResponses, nil
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
