package metrics

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rudder-load/internal/parser"
)

func TestMetricsClient_Query(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		time          int64
		mockResponse  QueryResponse
		mockStatus    int
		expectedError string
	}{
		{
			name:  "successful query",
			query: "test_query",
			time:  1234567890,
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "vector",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{
						{
							Metric: map[string]string{"label": "value"},
							Value:  []interface{}{1234567890, "42"},
						},
					},
				},
			},
			mockStatus: http.StatusOK,
		},
		{
			name:          "failed query",
			query:         "test_query",
			time:          1234567890,
			mockResponse:  QueryResponse{Status: "error"},
			mockStatus:    http.StatusOK,
			expectedError: "query failed with status: error",
		},
		{
			name:          "server error",
			query:         "test_query",
			time:          1234567890,
			mockResponse:  QueryResponse{},
			mockStatus:    http.StatusInternalServerError,
			expectedError: "unexpected status code: 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("expected POST request, got %s", r.Method)
				}
				if r.URL.Path != "/prometheus/api/v1/query" {
					t.Errorf("expected path /prometheus/api/v1/query, got %s", r.URL.Path)
				}
				if r.Header.Get("X-Scope-OrgID") != "allTenants" {
					t.Errorf("expected X-Scope-OrgID header 'allTenants', got %s", r.Header.Get("X-Scope-OrgID"))
				}

				w.WriteHeader(tt.mockStatus)
				json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewMimirClient(server.URL)
			resp, err := client.Query(context.Background(), tt.query, tt.time)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error to contain %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if resp.Status != tt.mockResponse.Status {
				t.Errorf("expected status %q, got %q", tt.mockResponse.Status, resp.Status)
			}
		})
	}
}

func TestMetricsClient_QueryRange(t *testing.T) {
	tests := []struct {
		name          string
		query         string
		start         int64
		end           int64
		step          string
		mockResponse  QueryResponse
		mockStatus    int
		expectedError string
	}{
		{
			name:  "successful range query",
			query: "test_query",
			start: 1234567890,
			end:   1234567899,
			step:  "1s",
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "matrix",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{
						{
							Metric: map[string]string{"label": "value"},
							Values: [][]interface{}{
								{1234567890, "42"},
								{1234567891, "43"},
							},
						},
					},
				},
			},
			mockStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					t.Errorf("expected POST request, got %s", r.Method)
				}
				if r.URL.Path != "/prometheus/api/v1/query_range" {
					t.Errorf("expected path /prometheus/api/v1/query_range, got %s", r.URL.Path)
				}

				w.WriteHeader(tt.mockStatus)
				json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewMimirClient(server.URL)
			resp, err := client.QueryRange(context.Background(), tt.query, tt.start, tt.end, tt.step)

			if tt.expectedError != "" {
				if err == nil || err.Error() != tt.expectedError {
					t.Errorf("expected error %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if resp.Status != tt.mockResponse.Status {
				t.Errorf("expected status %q, got %q", tt.mockResponse.Status, resp.Status)
			}
		})
	}
}

func TestMetricsClient_GetMetrics(t *testing.T) {
	tests := []struct {
		name                string
		metrics             []parser.Metric
		mockResponse        QueryResponse
		mockStatus          int
		expectedMetricKey   string
		expectedMetricValue float64
		expectedError       string
	}{
		{
			name: "successful RPS query",
			metrics: []parser.Metric{
				{Name: "rps"},
			},
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "vector",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{
						{
							Value: []interface{}{1234567890, "42.5"},
						},
					},
				},
			},
			mockStatus:          http.StatusOK,
			expectedMetricKey:   "rps",
			expectedMetricValue: 43, // Rounded up from 42.5
		},
		{
			name: "custom metric",
			metrics: []parser.Metric{
				{Name: "custom_metric"},
			},
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "vector",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{
						{
							Value: []interface{}{1234567890, "42.5"},
						},
					},
				},
			},
			mockStatus:          http.StatusOK,
			expectedMetricKey:   "custom_metric",
			expectedMetricValue: 43, // Rounded up from 42.5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatus)
				json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewMimirClient(server.URL)
			responses, err := client.GetMetrics(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				if err == nil || err.Error() != tt.expectedError {
					t.Errorf("expected error %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(responses) == 0 {
				t.Error("expected at least one response")
				return
			}

			var metricResp *MetricsResponse
			for _, resp := range responses {
				if resp.Key == tt.expectedMetricKey {
					metricResp = &resp
					break
				}
			}

			if metricResp == nil {
				t.Error("RPS metric not found in response")
				return
			}

			if metricResp.Value != tt.expectedMetricValue {
				t.Errorf("expected RPS %f, got %f", tt.expectedMetricValue, metricResp.Value)
			}
		})
	}
}

func TestMimirClient_Query_ErrorCases(t *testing.T) {
	tests := []struct {
		name          string
		mockResponse  interface{}
		mockStatus    int
		expectedError string
		query         string
		time          int64
		baseURL       string
	}{
		{
			name:          "invalid URL",
			baseURL:       "http://invalid:9898",
			query:         "test_query",
			time:          time.Now().Unix(),
			expectedError: "failed to execute request",
		},
		{
			name:          "unauthorized",
			mockStatus:    http.StatusUnauthorized,
			query:         "test_query",
			time:          time.Now().Unix(),
			expectedError: "unexpected status code: 401",
		},
		{
			name:          "server error",
			mockStatus:    http.StatusInternalServerError,
			query:         "test_query",
			time:          time.Now().Unix(),
			expectedError: "unexpected status code: 500",
		},
		{
			name:          "invalid JSON response",
			mockResponse:  "invalid json",
			mockStatus:    http.StatusOK,
			query:         "test_query",
			time:          time.Now().Unix(),
			expectedError: "failed to unmarshal response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var client *mimirClient
			if tt.baseURL != "" {
				client = NewMimirClient(tt.baseURL).(*mimirClient)
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					if tt.mockResponse != nil {
						switch v := tt.mockResponse.(type) {
						case string:
							w.Write([]byte(v))
						default:
							json.NewEncoder(w).Encode(v)
						}
					}
				}))
				defer server.Close()
				client = NewMimirClient(server.URL).(*mimirClient)
			}

			_, err := client.Query(context.Background(), tt.query, tt.time)

			if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
			}
		})
	}
}

func TestMimirClient_GetMetrics_Extended(t *testing.T) {
	tests := []struct {
		name              string
		metrics           []parser.Metric
		mockResponse      interface{}
		mockStatus        int
		expectedResponses []MetricsResponse
		expectedError     string
	}{
		{
			name:    "empty metric list",
			metrics: []parser.Metric{},
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "vector",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{},
				},
			},
			mockStatus:        http.StatusOK,
			expectedResponses: []MetricsResponse{},
		},
		{
			name: "multiple metrics",
			metrics: []parser.Metric{
				{Name: "rps", Query: "sum(rate(rudder_load_publish_duration_seconds_count[1m]))"},
				{Name: "custom_metric", Query: "custom_query"},
			},
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "vector",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{
						{
							Value: []interface{}{1234567890, "42.5"},
						},
					},
				},
			},
			mockStatus: http.StatusOK,
			expectedResponses: []MetricsResponse{
				{Key: "rps", Value: 43},
				{Key: "custom_metric", Value: 43},
			},
		},
		{
			name: "metric with no data",
			metrics: []parser.Metric{
				{Name: "no_data_metric", Query: "no_data_query"},
			},
			mockResponse: QueryResponse{
				Status: "success",
				Data: struct {
					ResultType string `json:"resultType"`
					Result     []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					} `json:"result"`
				}{
					ResultType: "vector",
					Result: []struct {
						Metric map[string]string `json:"metric"`
						Value  []interface{}     `json:"value"`
						Values [][]interface{}   `json:"values"`
					}{},
				},
			},
			mockStatus:        http.StatusOK,
			expectedResponses: []MetricsResponse{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.mockStatus)
				json.NewEncoder(w).Encode(tt.mockResponse)
			}))
			defer server.Close()

			client := NewMimirClient(server.URL)
			responses, err := client.GetMetrics(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(responses) != len(tt.expectedResponses) {
				t.Errorf("expected %d responses, got %d", len(tt.expectedResponses), len(responses))
				return
			}

			for i, expected := range tt.expectedResponses {
				if responses[i].Key != expected.Key {
					t.Errorf("response[%d]: expected key %q, got %q", i, expected.Key, responses[i].Key)
				}
				if math.IsNaN(expected.Value) {
					if !math.IsNaN(responses[i].Value) {
						t.Errorf("response[%d]: expected NaN value, got %f", i, responses[i].Value)
					}
				} else if responses[i].Value != expected.Value {
					t.Errorf("response[%d]: expected value %f, got %f", i, expected.Value, responses[i].Value)
				}
			}
		})
	}
}

func TestLocalMetricsClient_GetMetrics(t *testing.T) {
	tests := []struct {
		name              string
		metrics           []parser.Metric
		metricsData       string
		expectedResponses []MetricsResponse
		expectedError     string
	}{
		{
			name:              "empty metric list",
			metrics:           []parser.Metric{},
			metricsData:       "",
			expectedResponses: []MetricsResponse{},
		},
		{
			name: "exact metric match",
			metrics: []parser.Metric{
				{Name: "test_metric", Query: ""},
			},
			metricsData: "test_metric{label=\"value\"} 42.5",
			expectedResponses: []MetricsResponse{
				{Key: "test_metric", Value: 42.5},
			},
		},
		{
			name: "no partial match",
			metrics: []parser.Metric{
				{Name: "test", Query: ""},
			},
			metricsData:       "test_metric{label=\"value\"} 42.5\ntest_another{label=\"value\"} 10.0",
			expectedResponses: []MetricsResponse{},
		},
		{
			name: "multiple exact matches",
			metrics: []parser.Metric{
				{Name: "metric1", Query: ""},
				{Name: "metric2", Query: ""},
			},
			metricsData: "metric1{label1=\"value1\",label2=\"value2\"} 42.5\nmetric2{label=\"value\"} 10.0",
			expectedResponses: []MetricsResponse{
				{Key: "metric1", Value: 42.5},
				{Key: "metric2", Value: 10.0},
			},
		},
		{
			name: "invalid metric format",
			metrics: []parser.Metric{
				{Name: "test", Query: ""},
			},
			metricsData:   "test_metric invalid_value",
			expectedError: "failed to parse metric value",
		},
		{
			name: "missing metric value",
			metrics: []parser.Metric{
				{Name: "test", Query: ""},
			},
			metricsData:       "test_metric{label=\"value\"}",
			expectedResponses: []MetricsResponse{},
		},
		{
			name: "non-numeric metric value",
			metrics: []parser.Metric{
				{Name: "test_metric", Query: ""},
			},
			metricsData: "test_metric{label=\"value\"} NaN",
			expectedResponses: []MetricsResponse{
				{Key: "test_metric", Value: math.NaN()},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server that returns the metrics data
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(tt.metricsData))
			}))
			defer server.Close()

			client := NewLocalMetricsClient(server.URL)
			responses, err := client.GetMetrics(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(responses) != len(tt.expectedResponses) {
				t.Errorf("expected %d responses, got %d", len(tt.expectedResponses), len(responses))
				return
			}

			for i, expected := range tt.expectedResponses {
				if responses[i].Key != expected.Key {
					t.Errorf("response[%d]: expected key %q, got %q", i, expected.Key, responses[i].Key)
				}
				if math.IsNaN(expected.Value) {
					if !math.IsNaN(responses[i].Value) {
						t.Errorf("response[%d]: expected NaN value, got %f", i, responses[i].Value)
					}
				} else if responses[i].Value != expected.Value {
					t.Errorf("response[%d]: expected value %f, got %f", i, expected.Value, responses[i].Value)
				}
			}
		})
	}
}

func TestLocalMetricsClient_UnsupportedMethods(t *testing.T) {
	client := NewLocalMetricsClient("")
	ctx := context.Background()

	t.Run("Query method", func(t *testing.T) {
		_, err := client.Query(ctx, "test_query", time.Now().Unix())
		if err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Errorf("expected error about unsupported method, got %v", err)
		}
	})
	t.Run("QueryRange method", func(t *testing.T) {
		_, err := client.QueryRange(ctx, "test_query", time.Now().Add(-1*time.Hour).Unix(), time.Now().Unix(), "1m")
		if err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Errorf("expected error about unsupported method, got %v", err)
		}
	})
}
