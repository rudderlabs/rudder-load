package envvar

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnvVarFlag_Set(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:    "valid input",
			input:   "KEY=value",
			want:    map[string]string{"KEY": "value"},
			wantErr: false,
		},
		{
			name:    "valid input with equals sign in value",
			input:   "KEY=value=with=equals",
			want:    map[string]string{"KEY": "value=with=equals"},
			wantErr: false,
		},
		{
			name:    "invalid input - no equals sign",
			input:   "KEYvalue",
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewEnvVarFlag()
			err := e.Set(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.Equal(t, e.Values, tt.want)
		})
	}
}

func TestEnvVarFlag_String(t *testing.T) {
	e := NewEnvVarFlag()
	e.Values["KEY1"] = "value1"
	e.Values["KEY2"] = "value2"

	result := e.String()
	expected := "map[KEY1:value1 KEY2:value2]"

	require.Equal(t, result, expected)
}

func TestEnvVarFlag_GetValues(t *testing.T) {
	e := NewEnvVarFlag()
	e.Values["KEY1"] = "value1"
	e.Values["KEY2"] = "value2"

	result := e.GetValues()
	expected := map[string]string{"KEY1": "value1", "KEY2": "value2"}

	require.Equal(t, result, expected)
}
