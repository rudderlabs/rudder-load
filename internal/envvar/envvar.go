package envvar

import (
	"fmt"
	"strings"
)

type EnvVarFlag struct {
	Values map[string]string
}

func NewEnvVarFlag() *EnvVarFlag {
	return &EnvVarFlag{
		Values: make(map[string]string),
	}
}

func (e *EnvVarFlag) String() string {
	return fmt.Sprintf("%v", e.Values)
}

func (e *EnvVarFlag) Set(value string) error {
	if e.Values == nil {
		e.Values = make(map[string]string)
	}

	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid environment variable format: %s (expected KEY=VALUE)", value)
	}

	e.Values[parts[0]] = parts[1]
	return nil
}

// GetValues returns the map of environment variables
func (e *EnvVarFlag) GetValues() map[string]string {
	return e.Values
}
