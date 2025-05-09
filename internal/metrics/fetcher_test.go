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

	"github.com/stretchr/testify/require"
)

func TestMetricsFetcher_Query(t *testing.T) {
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
				require.Equal(t, "POST", r.Method, "expected POST request")
				require.Equal(t, "/prometheus/api/v1/query", r.URL.Path, "expected correct path")
				require.Equal(t, "allTenants", r.Header.Get("X-Scope-OrgID"), "expected correct header")

				w.WriteHeader(tt.mockStatus)
				err := json.NewEncoder(w).Encode(tt.mockResponse)
				require.NoError(t, err)
			}))
			defer server.Close()

			metricsFetcher := NewMetricsFetcher(server.URL)
			resp, err := metricsFetcher.Query(context.Background(), tt.query, tt.time)

			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.mockResponse.Status, resp.Status, "response status mismatch")
		})
	}
}

func TestMetricsFetcher_QueryRange(t *testing.T) {
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
				require.Equal(t, "POST", r.Method, "expected POST request")
				require.Equal(t, "/prometheus/api/v1/query_range", r.URL.Path, "expected correct path")

				w.WriteHeader(tt.mockStatus)
				err := json.NewEncoder(w).Encode(tt.mockResponse)
				require.NoError(t, err)
			}))
			defer server.Close()

			metricsFetcher := NewMetricsFetcher(server.URL)
			resp, err := metricsFetcher.QueryRange(context.Background(), tt.query, tt.start, tt.end, tt.step)

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Equal(t, tt.expectedError, err.Error())
				return
			}

			require.NoError(t, err)

			if resp.Status != tt.mockResponse.Status {
				t.Errorf("expected status %q, got %q", tt.mockResponse.Status, resp.Status)
			}
		})
	}
}

func TestMetricsFetcher_GetMetrics(t *testing.T) {
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
				err := json.NewEncoder(w).Encode(tt.mockResponse)
				require.NoError(t, err)
			}))
			defer server.Close()

			metricsFetcher := NewMetricsFetcher(server.URL)
			responses, err := metricsFetcher.GetMetrics(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				require.Error(t, err)
				require.Equal(t, tt.expectedError, err.Error())
				return
			}

			require.NoError(t, err)
			require.NotEmpty(t, responses, "expected at least one response")

			var metricResp *MetricsResponse
			for _, resp := range responses {
				if resp.Key == tt.expectedMetricKey {
					metricResp = &resp
					break
				}
			}

			require.NotNil(t, metricResp, "RPS metric not found in response")
			require.Equal(t, tt.expectedMetricValue, metricResp.Value, "RPS value mismatch")
		})
	}
}

func TestMetricsFetcher_Query_ErrorCases(t *testing.T) {
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
			var metricsFetcher *Fetcher
			if tt.baseURL != "" {
				metricsFetcher = NewMetricsFetcher(tt.baseURL)
			} else {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.mockStatus)
					if tt.mockResponse != nil {
						switch v := tt.mockResponse.(type) {
						case string:
							w.Write([]byte(v))
						default:
							err := json.NewEncoder(w).Encode(v)
							require.NoError(t, err)
						}
					}
				}))
				defer server.Close()
				metricsFetcher = NewMetricsFetcher(server.URL)
			}

			_, err := metricsFetcher.Query(context.Background(), tt.query, tt.time)
			require.ErrorContains(t, err, tt.expectedError)
		})
	}
}

func TestMetricsFetcher_GetMetrics_Extended(t *testing.T) {
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
				err := json.NewEncoder(w).Encode(tt.mockResponse)
				require.NoError(t, err)
			}))
			defer server.Close()

			metricsFetcher := NewMetricsFetcher(server.URL)
			responses, err := metricsFetcher.GetMetrics(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				require.ErrorContains(t, err, tt.expectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, len(tt.expectedResponses), len(responses), "number of responses mismatch")

			for i, expected := range tt.expectedResponses {
				require.Equal(t, expected.Key, responses[i].Key, "response key mismatch")
				if math.IsNaN(expected.Value) {
					require.True(t, math.IsNaN(responses[i].Value), "expected NaN value")
				} else {
					require.Equal(t, expected.Value, responses[i].Value, "response value mismatch")
				}
			}
		})
	}
}

func TestLocalMetricsFetcher_GetMetrics(t *testing.T) {
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
			// TODO: create actual Prometheus server and test against it
			// Create a test server that returns the metrics data
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(tt.metricsData))
			}))
			defer server.Close()

			metricsFetcher := NewLocalMetricsFetcher(server.URL)
			responses, err := metricsFetcher.GetMetrics(context.Background(), tt.metrics)

			if tt.expectedError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
				}
				return
			}

			require.NoError(t, err)

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

func TestLocalMetricsFetcher_UnsupportedMethods(t *testing.T) {
	metricsFetcher := NewLocalMetricsFetcher("")
	ctx := context.Background()

	t.Run("Query method", func(t *testing.T) {
		_, err := metricsFetcher.Query(ctx, "test_query", time.Now().Unix())
		require.ErrorContains(t, err, "not supported")
	})
	t.Run("QueryRange method", func(t *testing.T) {
		_, err := metricsFetcher.QueryRange(ctx, "test_query", time.Now().Add(-1*time.Hour).Unix(), time.Now().Unix(), "1m")
		require.ErrorContains(t, err, "not supported")
	})
}
