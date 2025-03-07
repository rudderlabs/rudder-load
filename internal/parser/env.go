package parser

import (
	"fmt"

	"github.com/joho/godotenv"
)

func LoadEnvConfig() map[string]string {
	// Load .env file
	envVars, err := godotenv.Read(".env")
	if err != nil {
		return map[string]string{}
	}

	fmt.Printf("envVars: %+v\n", envVars)

	return envVars
}

func MergeEnvVars(configEnvVars, envFileVars map[string]string) map[string]string {
	result := make(map[string]string)

	for key, value := range envFileVars {
		result[key] = value
	}

	for key, value := range configEnvVars {
		result[key] = value
	}

	return result
}
