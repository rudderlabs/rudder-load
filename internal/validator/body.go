package validator

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// BodyValidator defines a function type that validates a request body and returns a validation result and an error.
type BodyValidator func(body []byte) (bool, error)

func ValidateResponseBody(validatorType string) BodyValidator {
	switch validatorType {
	case "user-transformer-hash-email":
		return userTransformerHashEmail
	}
	return nil
}

var userTransformerHashEmail BodyValidator = func(body []byte) (bool, error) {
	var parsedBody userTransformerHashEmailResponse
	if err := json.Unmarshal(body, &parsedBody); err != nil {
		return false, fmt.Errorf("invalid response body JSON: %w", err)
	}
	if len(parsedBody) == 0 {
		return false, fmt.Errorf("response body array is empty")
	}
	for _, item := range parsedBody {
		if item.StatusCode != http.StatusOK {
			return false, fmt.Errorf("response status code is not 200")
		}
		if item.Output.Context.Traits.Email == "" {
			return false, fmt.Errorf("email trait is missing")
		}
		if !sha256Validator.MatchString(item.Output.Context.Traits.Email) {
			return false, fmt.Errorf("email hash must be a valid sha256 hexadecimal string")
		}
	}
	return true, nil
}

// Define the expected JSON structure to parse
type userTransformerHashEmailResponse []struct {
	Output struct {
		Context struct {
			Traits struct {
				Email string `json:"email"`
			} `json:"traits"`
		} `json:"context"`
	} `json:"output"`
	StatusCode int `json:"statusCode"`
}
