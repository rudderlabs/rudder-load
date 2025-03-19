package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOptionalMap(t *testing.T) {
	tests := []struct {
		name     string
		envKey   string
		envValue string
		items    []string
		want     []int
		wantErr  bool
	}{
		{
			name:     "empty env returns equal distribution",
			envKey:   "TEST_MAP",
			envValue: "",
			items:    []string{"a", "b"},
			want:     []int{50, 50},
			wantErr:  false,
		},
		{
			name:     "empty env with 3 items",
			envKey:   "TEST_MAP",
			envValue: "",
			items:    []string{"a", "b", "c"},
			want:     []int{34, 33, 33},
			wantErr:  false,
		},
		{
			name:     "custom distribution",
			envKey:   "TEST_MAP",
			envValue: "60,40",
			items:    []string{"a", "b"},
			want:     []int{60, 40},
			wantErr:  false,
		},
		{
			name:    "empty items slice",
			envKey:  "TEST_MAP",
			items:   []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(tt.envKey, tt.envValue)
				defer os.Unsetenv(tt.envKey)
			}

			if tt.wantErr {
				assert.Panics(t, func() { optionalMap(tt.envKey, tt.items) })
				return
			}

			got := optionalMap(tt.envKey, tt.items)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestConvertToBytes(t *testing.T) {
	tests := []struct {
		input   string
		want    int
		wantErr bool
	}{
		{"1kb", 1000, false},
		{"1mb", 1000000, false},
		{"1gb", 1000000000, false},
		{"1kib", 1024, false},
		{"1mib", 1048576, false},
		{"1gi", 1073741824, false},
		{"invalid", 0, true},
		{"1xx", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := convertToBytes(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetUserIDsConcentration(t *testing.T) {
	tests := []struct {
		name       string
		totalUsers int
		hotGroups  []int
		random     bool
		wantLen    int
		wantPanic  bool
	}{
		{
			name:       "valid distribution",
			totalUsers: 100,
			hotGroups:  []int{60, 40},
			wantLen:    100,
		},
		{
			name:       "invalid percentage sum",
			totalUsers: 100,
			hotGroups:  []int{60, 30},
			wantPanic:  true,
		},
		{
			name:       "invalid total users",
			totalUsers: 99,
			hotGroups:  []int{60, 40},
			wantPanic:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				assert.Panics(t, func() {
					getUserIDsConcentration(tt.totalUsers, tt.hotGroups, tt.random)
				})
				return
			}

			got := getUserIDsConcentration(tt.totalUsers, tt.hotGroups, tt.random)
			assert.Len(t, got, tt.wantLen)

			// Test that functions return valid UUIDs
			if tt.random {
				result := got[0]()
				assert.Len(t, result, 36) // UUID length
			}
		})
	}
}

func TestGetBatchSizesConcentration(t *testing.T) {
	tests := []struct {
		name       string
		batchSizes []int
		hotSizes   []int
		want       []int
		wantPanic  bool
	}{
		{
			name:       "valid distribution",
			batchSizes: []int{1, 2},
			hotSizes:   []int{60, 40},
			want:       make([]int, 100), // Will be filled with 60 1s and 40 2s
		},
		{
			name:       "invalid percentage sum",
			batchSizes: []int{1, 2},
			hotSizes:   []int{60, 30},
			wantPanic:  true,
		},
		{
			name:       "mismatched lengths",
			batchSizes: []int{1},
			hotSizes:   []int{60, 40},
			wantPanic:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				assert.Panics(t, func() {
					getBatchSizesConcentration(tt.batchSizes, tt.hotSizes)
				})
				return
			}

			got := getBatchSizesConcentration(tt.batchSizes, tt.hotSizes)
			assert.Len(t, got, 100)

			// Verify distribution
			ones := 0
			for _, v := range got {
				if v == 1 {
					ones++
				}
			}
			assert.Equal(t, 60, ones) // Verify 60% are 1s
		})
	}
}

func TestGetSourcesConcentration(t *testing.T) {
	tests := []struct {
		name       string
		sources    []string
		hotSources []int
		wantPanic  bool
		validate   func(t *testing.T, result []func() string)
	}{
		{
			name:       "valid distribution 60/40",
			sources:    []string{"source1", "source2"},
			hotSources: []int{60, 40},
			validate: func(t *testing.T, result []func() string) {
				// Count occurrences of each source
				counts := make(map[string]int)
				for _, f := range result {
					counts[f()]++
				}

				assert.Equal(t, 60, counts["source1"], "source1 should appear 60 times")
				assert.Equal(t, 40, counts["source2"], "source2 should appear 40 times")
				assert.Equal(t, 100, len(result), "should have 100 functions")
			},
		},
		{
			name:       "three sources distribution",
			sources:    []string{"source1", "source2", "source3"},
			hotSources: []int{50, 30, 20},
			validate: func(t *testing.T, result []func() string) {
				counts := make(map[string]int)
				for _, f := range result {
					counts[f()]++
				}

				assert.Equal(t, 50, counts["source1"])
				assert.Equal(t, 30, counts["source2"])
				assert.Equal(t, 20, counts["source3"])
			},
		},
		{
			name:       "panic on percentage not 100",
			sources:    []string{"source1", "source2"},
			hotSources: []int{60, 30}, // Only sums to 90
			wantPanic:  true,
		},
		{
			name:       "panic on length mismatch",
			sources:    []string{"source1"},
			hotSources: []int{60, 40}, // More hot sources than sources
			wantPanic:  true,
		},
		{
			name:       "single source 100%",
			sources:    []string{"source1"},
			hotSources: []int{100},
			validate: func(t *testing.T, result []func() string) {
				for _, f := range result {
					assert.Equal(t, "source1", f())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				assert.Panics(t, func() {
					getSourcesConcentration(tt.sources, tt.hotSources)
				})
				return
			}

			result := getSourcesConcentration(tt.sources, tt.hotSources)
			assert.NotNil(t, result)
			assert.Equal(t, 100, len(result), "should always return 100 functions")

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}
