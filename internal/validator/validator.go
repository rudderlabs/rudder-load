package validator

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"rudder-load/internal/parser"
)

var (
	namespaceValidator    = regexp.MustCompile(`^[a-z0-9-]+$`)
	loadNameValidator     = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	durationValidator     = regexp.MustCompile(`^(\d+[hms])+$`)
	httpEndpointValidator = regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(:\d+)?(/.*)?$`)
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

func ValidateSources(sources string) error {
	v := strings.Split(sources, ",")
	for _, source := range v {
		if strings.TrimSpace(source) == "" {
			return fmt.Errorf("invalid sources: contains empty source: %s", sources)
		}
	}
	return nil
}

func ValidateHotSources(hotSources string) error {
	totalPercentage := 0
	values := strings.Split(hotSources, ",")
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("invalid hot sources: %s", hotSources)
		}
		percentage, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("hot sources percentage must be an integer: %s", hotSources)
		}
		if percentage < 0 || percentage > 100 {
			return fmt.Errorf("hot sources percentage must be between 0 and 100: %s", hotSources)
		}
		totalPercentage += percentage
	}
	if totalPercentage != 100 {
		return fmt.Errorf("hot sources percentages must sum up to 100: %s", hotSources)
	}
	return nil
}

func ValidateHotSourcesDistribution(source string, hotSources string) error {
	sourceValues := strings.Split(source, ",")
	hotSourceValues := strings.Split(hotSources, ",")
	if len(sourceValues) != len(hotSourceValues) {
		return fmt.Errorf("sources and hot sources must have the same length: %s, %s", source, hotSources)
	}

	return nil
}

func ValidateHttpEndpoint(endpoint string) error {
	if !httpEndpointValidator.MatchString(endpoint) {
		return fmt.Errorf("invalid http endpoint: %s", endpoint)
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
	if err := ValidateSources(os.Getenv("SOURCES")); err != nil {
		return err
	}
	if hotSources, ok := config.EnvOverrides["HOT_SOURCES"]; ok {
		if err := ValidateHotSources(hotSources); err != nil {
			return err
		}
		if err := ValidateHotSourcesDistribution(os.Getenv("SOURCES"), hotSources); err != nil {
			return err
		}
	}
	if err := ValidateHttpEndpoint(os.Getenv("HTTP_ENDPOINT")); err != nil {
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
