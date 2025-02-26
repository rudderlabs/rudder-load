package validator

import (
	"fmt"
	"regexp"

	"rudder-load/internal/parser"
)

var (
	namespaceValidator = regexp.MustCompile(`^[a-z0-9-]+$`)
	loadNameValidator  = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	durationValidator  = regexp.MustCompile(`^(\d+[hms])+$`)
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
