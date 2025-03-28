package main

import (
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventGenerators(t *testing.T) {
	t.Run("page generator", func(t *testing.T) {
		tmpl, err := template.New("page").Parse(`{"type": "page", "userId": "{{.AnonymousID}}", "loadRunId": "{{.LoadRunID}}", "noOfEvents": {{.NoOfEvents}}}`)
		require.NoError(t, err)

		result := pageFunc(tmpl, "test-user", "test-run", 2, nil)
		output := string(result)

		require.Contains(t, output, `"userId": "test-user"`)
		require.Contains(t, output, `"loadRunId": "test-run"`)
		require.Contains(t, output, `"noOfEvents": 2`)
	})

	t.Run("track generator", func(t *testing.T) {
		tmpl, err := template.New("track").Parse(`{"type": "track", "errorProbability": 0.5, "userId": "{{.UserID}}", "event": "{{.Event}}", "loadRunId": "{{.LoadRunID}}", "noOfEvents": {{.NoOfEvents}}}`)
		require.NoError(t, err)

		result := trackFunc(tmpl, "test-user", "test-run", 1, nil)
		output := string(result)

		require.Contains(t, output, `"userId": "test-user"`)
		require.Contains(t, output, `"loadRunId": "test-run"`)
		require.Contains(t, output, `"noOfEvents": 1`)
		require.Contains(t, output, `"errorProbability": 0.5`)
	})

	t.Run("identify generator", func(t *testing.T) {
		tmpl, err := template.New("identify").Parse(`{"type": "identify", "anonymousId": "{{.AnonymousID}}", "loadRunId": "{{.LoadRunID}}", "noOfEvents": {{.NoOfEvents}}}`)
		require.NoError(t, err)

		result := identifyFunc(tmpl, "test-user", "test-run", 3, nil)
		output := string(result)

		require.Contains(t, output, `"anonymousId": "test-user"`)
		require.Contains(t, output, `"loadRunId": "test-run"`)
		require.Contains(t, output, `"noOfEvents": 3`)
	})
}

func TestGetEventTypesConcentration(t *testing.T) {
	t.Run("valid distribution", func(t *testing.T) {
		eventTypes := []eventType{
			{Type: "track", Values: nil},
			{Type: "page", Values: nil},
		}
		hotEventTypes := []int{60, 40}

		generators := map[string]eventGenerator{
			"track": func(t *template.Template, userID, loadRunID string, n int, values []int) []byte {
				return []byte("track")
			},
			"page": func(t *template.Template, userID, loadRunID string, n int, values []int) []byte {
				return []byte("page")
			},
		}

		templates := map[string]*template.Template{
			"track": template.New("track"),
			"page":  template.New("page"),
		}

		concentration := getEventTypesConcentration("test-run", eventTypes, hotEventTypes, generators, templates)
		require.Len(t, concentration, 100)

		trackCount := 0
		pageCount := 0
		for _, f := range concentration {
			result := string(f("test-user", 1))
			switch result {
			case "track":
				trackCount++
			case "page":
				pageCount++
			}
		}

		require.Equal(t, 60, trackCount)
		require.Equal(t, 40, pageCount)
	})

	t.Run("panic on percentage not 100", func(t *testing.T) {
		assert.Panics(t, func() {
			getEventTypesConcentration("test", []eventType{{Type: "track"}}, []int{60}, nil, nil)
		})
	})

	t.Run("panic on length mismatch", func(t *testing.T) {
		assert.Panics(t, func() {
			getEventTypesConcentration("test",
				[]eventType{{Type: "track"}, {Type: "page"}},
				[]int{100},
				nil, nil)
		})
	})
}

func TestRegisterCustomEventGenerators(t *testing.T) {
	tests := []struct {
		name       string
		eventTypes []eventType
		want       []string // expected keys in eventGenerators
	}{
		{
			name: "register single custom event",
			eventTypes: []eventType{
				{Type: "custom_purchase", Values: nil},
			},
			want: []string{"custom_purchase"},
		},
		{
			name: "register multiple custom events",
			eventTypes: []eventType{
				{Type: "custom_login", Values: nil},
				{Type: "custom_signup", Values: nil},
			},
			want: []string{"custom_login", "custom_signup"},
		},
		{
			name: "register duplicate custom events",
			eventTypes: []eventType{
				{Type: "custom_login", Values: nil},
				{Type: "custom_login", Values: nil},
			},
			want: []string{"custom_login"},
		},
		{
			name: "ignore non-custom events",
			eventTypes: []eventType{
				{Type: "page", Values: nil},
				{Type: "custom_event", Values: nil},
				{Type: "track", Values: nil},
			},
			want: []string{"custom_event"},
		},
		{
			name:       "handle empty event types",
			eventTypes: []eventType{},
			want:       []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Store original eventGenerators to restore after test
			originalGenerators := make(map[string]eventGenerator)
			for k, v := range eventGenerators {
				originalGenerators[k] = v
			}

			// Reset eventGenerators to known state before each test
			eventGenerators = map[string]eventGenerator{
				"page":     pageFunc,
				"track":    trackFunc,
				"identify": identifyFunc,
			}

			// Run the function
			registerCustomEventGenerators(tt.eventTypes)

			// Check if all expected custom events were registered
			for _, expectedType := range tt.want {
				if _, exists := eventGenerators[expectedType]; !exists {
					t.Errorf("expected event generator for %s to be registered, but it wasn't", expectedType)
				}
			}

			// Verify the registered generator works
			for _, expectedType := range tt.want {
				generator := eventGenerators[expectedType]
				tmpl := template.Must(template.New("test").Parse("{{.Event}}"))

				result := generator(tmpl, "test-user", "test-run", 1, nil)
				if string(result) != expectedType {
					t.Errorf("generator for %s produced incorrect event name, got %s", expectedType, string(result))
				}
			}

			// Restore original eventGenerators
			eventGenerators = originalGenerators
		})
	}
}
