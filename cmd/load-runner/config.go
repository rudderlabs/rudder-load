package main

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type LoadTestConfig struct {
	Name          string     `yaml:"name"`
	Namespace     string     `yaml:"namespace"`
	ChartFilePath string     `yaml:"chartFilePath"`
	Phases        []RunPhase `yaml:"phases"`

	ReleaseName string
	FromFile    bool
}

type RunPhase struct {
	Duration string `yaml:"duration"`
	Replicas int    `yaml:"replicas"`
}

func ParseLoadTestConfig(args *CLIArgs) (*LoadTestConfig, error) {
	var cfg LoadTestConfig
	if args.testFile == "" {
		cfg.Name = args.loadName
		cfg.Namespace = args.namespace
		cfg.ChartFilePath = args.chartFilesPath
		cfg.Phases = []RunPhase{
			{Duration: args.duration, Replicas: 1},
		}
		cfg.FromFile = false
		return &cfg, nil

	}
	data, err := os.Open(args.testFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read test file: %w", err)
	}
	defer data.Close()

	decoder := yaml.NewDecoder(data)
	err = decoder.Decode(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to decode test file: %w", err)
	}
	cfg.FromFile = true
	return &cfg, nil
}

func (c *LoadTestConfig) Validate() error {
	if !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(c.Namespace) {
		return fmt.Errorf("namespace must contain only lowercase alphanumeric characters and '-'")
	}

	if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(c.Name) {
		return fmt.Errorf("load name must contain only alphanumeric characters and '-'")
	}

	for _, phase := range c.Phases {
		if !regexp.MustCompile(`^(\d+[hms])+$`).MatchString(phase.Duration) {
			return fmt.Errorf("duration must include 'h', 'm', or 's' (e.g., '1h30m')")
		}
		if phase.Replicas <= 0 {
			return fmt.Errorf("replicas must be greater than 0")
		}
	}

	return nil
}

func (c *LoadTestConfig) SetDefaults() {
	const (
		defaultReleaseNamePrefix = "rudder-load"
		defaultChartFilesPath    = "./artifacts/helm"
	)

	c.ReleaseName = fmt.Sprintf("%s-%s", defaultReleaseNamePrefix, c.Name)
	if c.ChartFilePath == "" {
		c.ChartFilePath = defaultChartFilesPath
	}
}
