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
	GetMetrics(ctx context.Context, mts []parser.Metric) (MetricsResponse, error)
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
	RPS float64
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

func (m *mimirClient) GetMetrics(ctx context.Context, mts []parser.Metric) (MetricsResponse, error) {
	var metricsResp MetricsResponse

	for _, metric := range mts {
		switch metric.Name {
		case "rps":
			rpsQuery := `sum(rate(rudder_load_publish_duration_seconds_count[1m]))`
			if metric.Query != "" {
				rpsQuery = metric.Query
			}
			rpsResp, err := m.Query(ctx, rpsQuery, time.Now().Unix())
			if err != nil {
				return metricsResp, fmt.Errorf("failed to query RPS: %w", err)
			}
			if len(rpsResp.Data.Result) > 0 {
				if str, ok := rpsResp.Data.Result[0].Value[1].(string); ok {
					metricsResp.RPS, err = strconv.ParseFloat(str, 64)
					if err != nil {
						return metricsResp, fmt.Errorf("failed to parse RPS value: %w", err)
					}
					metricsResp.RPS = math.Round(metricsResp.RPS)
				}
			}
		default:
			return metricsResp, fmt.Errorf("unknown metric: %s", metric.Name)
		}
	}
	return metricsResp, nil
}
