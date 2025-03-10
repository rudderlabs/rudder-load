package parser

import (
	"os"

	"github.com/joho/godotenv"
)

func LoadEnvConfig(filePath string) (map[string]string, error) {
	envVars, err := godotenv.Read(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	return envVars, nil
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
