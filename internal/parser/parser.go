package parser

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type CLIArgs struct {
	Duration       string
	Namespace      string
	LoadName       string
	ChartFilesPath string
	TestFile       string
}

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
	if args.TestFile == "" {
		cfg.Name = args.LoadName
		cfg.Namespace = args.Namespace
		cfg.ChartFilePath = args.ChartFilesPath
		cfg.Phases = []RunPhase{
			{Duration: args.Duration, Replicas: 1},
		}
		cfg.FromFile = false
		return &cfg, nil

	}
	data, err := os.Open(args.TestFile)
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
