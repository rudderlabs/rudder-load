package validator

import (
	"fmt"
	"net/http"
	"regexp"

	json "github.com/sugawarayuuta/sonnet"

	"rudder-load/internal/parser"
)

var (
	namespaceValidator = regexp.MustCompile(`^[a-z0-9-]+$`)
	loadNameValidator  = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	durationValidator  = regexp.MustCompile(`^(\d+[hms])+$`)
	sha256Validator    = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
)

func ValidateNamespace(namespace string) error {
	if !namespaceValidator.MatchString(namespace) {
		return fmt.Errorf("namespace must contain only lowercase alphanumeric characters and '-'")
	}
	return nil
}

func ValidateLoadName(name string) error {
	if !loadNameValidator.MatchString(name) {
		return fmt.Errorf("load name must contain only alphanumeric characters and '-'")
	}
	return nil
}

func ValidateDuration(duration string) error {
	if !durationValidator.MatchString(duration) {
		return fmt.Errorf("duration must include 'h', 'm', or 's' (e.g., '1h30m')")
	}
	return nil
}

func ValidateLoadTestConfig(config *parser.LoadTestConfig) error {
	if err := ValidateNamespace(config.Namespace); err != nil {
		return err
	}
	if err := ValidateLoadName(config.Name); err != nil {
		return err
	}
	for _, phase := range config.Phases {
		if err := ValidateDuration(phase.Duration); err != nil {
			return err
		}
		if phase.Replicas <= 0 {
			return fmt.Errorf("replicas must be greater than 0")
		}
	}
	return nil
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

func ValidateResponseBody(validatorType string) func(body []byte) (bool, error) {
	switch validatorType {
	case "user-transformer-hash-email":
		return func(body []byte) (bool, error) {
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
	}
	return nil
}
