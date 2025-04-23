package validator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateResponseBody(t *testing.T) {
	type testCase struct {
		name          string
		validatorType string
		input         []byte
		expected      bool
		error         string
	}
	testCases := []testCase{
		{
			name:          "user-transformer-hash-email-invalid-json",
			validatorType: "user-transformer-hash-email",
			input: []byte(`[
				{
					"output": {
						"context": {
							"traits": {
								"email": "94b6ed36d83948d1c1b5f968e52e1160050ff59821dcfc368fdcdf036cb6143f"
							}
						}
					"statusCode": 200
				}
			]`), // Missing closing braces leading to invalid JSON
			expected: false,
			error:    "invalid response body JSON",
		},
		{
			name:          "user-transformer-hash-email-empty-response-body",
			validatorType: "user-transformer-hash-email",
			input:         []byte(`[]`), // Empty response body
			expected:      false,
			error:         "response body array is empty",
		},
		{
			name:          "user-transformer-hash-email-not-200",
			validatorType: "user-transformer-hash-email",
			input: []byte(`[
				{
					"output": {
						"context": {
							"traits": {
								"email": "94b6ed36d83948d1c1b5f968e52e1160050ff59821dcfc368fdcdf036cb6143f"
							}
						}
					},
					"statusCode": 500
				}
			]`),
			expected: false,
			error:    "response status code is not 200",
		},
		{
			name:          "user-transformer-hash-email-missing-email-trait",
			validatorType: "user-transformer-hash-email",
			input: []byte(`[
				{
					"output": {
						"context": {
							"traits": {}
						}
					},
					"statusCode": 200
				}
			]`),
			expected: false,
			error:    "email trait is missing",
		},
		{
			name:          "user-transformer-hash-email-invalid-email-trait",
			validatorType: "user-transformer-hash-email",
			input: []byte(`[
				{
					"output": {
						"context": {
							"traits": {
								"email": "invalidhashvalue"
							}
						}
					},
					"statusCode": 200
				}
			]`),
			expected: false,
			error:    "email hash must be a valid sha256 hexadecimal string",
		},
		{
			name:          "user-transformer-hash-email-valid",
			validatorType: "user-transformer-hash-email",
			input: []byte(`[
				{
					"output": {
						"context": {
							"traits": {
								"email": "94b6ed36d83948d1c1b5f968e52e1160050ff59821dcfc368fdcdf036cb6143f"
							}
						}
					},
					"statusCode": 200
				}
			]`),
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := ValidateResponseBody(tc.validatorType)
			result, err := f(tc.input)
			require.Equal(t, tc.expected, result)
			if tc.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.error)
			}
		})
	}
}
