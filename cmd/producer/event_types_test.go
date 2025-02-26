package main

import (
	"testing"
	"text/template"
)

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
