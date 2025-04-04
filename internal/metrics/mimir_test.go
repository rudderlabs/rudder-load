package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rudder-load/internal/parser"
)

func TestMimirClient_Query(t *testing.T) {
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

func TestMimirClient_QueryRange(t *testing.T) {
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

func TestMimirClient_GetMetrics(t *testing.T) {
	tests := []struct {
		name          string
		metrics       []parser.Metric
		mockResponse  QueryResponse
		mockStatus    int
		expectedRPS   float64
		expectedError string
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
			mockStatus:  http.StatusOK,
			expectedRPS: 43, // Rounded up from 42.5
		},
		{
			name: "unknown metric",
			metrics: []parser.Metric{
				{Name: "unknown"},
			},
			expectedError: "unknown metric: unknown",
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
			resp, err := client.GetMetrics(context.Background(), tt.metrics)

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

			if resp.RPS != tt.expectedRPS {
				t.Errorf("expected RPS %f, got %f", tt.expectedRPS, resp.RPS)
			}
		})
	}
}
