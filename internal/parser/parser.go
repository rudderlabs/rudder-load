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
	EnvVars        map[string]string
	LocalExecution bool
}

type LoadTestConfig struct {
	Name          string            `yaml:"name"`
	Namespace     string            `yaml:"namespace"`
	ChartFilePath string            `yaml:"chartFilePath"`
	Phases        []RunPhase        `yaml:"phases"`
	EnvOverrides  map[string]string `yaml:"env"`
	Reporting     Reporting         `yaml:"reporting"`

	ReleaseName    string
	FromFile       bool
	LocalExecution bool
}

type RunPhase struct {
	Duration     string            `yaml:"duration"`
	Replicas     int               `yaml:"replicas"`
	EnvOverrides map[string]string `yaml:"env"`
}

type Reporting struct {
	Namespace string   `yaml:"namespace"`
	Interval  string   `yaml:"interval"`
	Metrics   []Metric `yaml:"metrics"`
}

type Metric struct {
	Name  string `yaml:"name"`
	Query string `yaml:"query"`
}

func ParseLoadTestConfig(args *CLIArgs) (*LoadTestConfig, error) {
	var cfg LoadTestConfig
	if args.LocalExecution {
		cfg.LocalExecution = args.LocalExecution
	}
	if args.TestFile == "" {
		cfg.Name = args.LoadName
		cfg.Namespace = args.Namespace
		cfg.ChartFilePath = args.ChartFilesPath
		cfg.Phases = []RunPhase{
			{Duration: args.Duration, Replicas: 1},
		}
		cfg.FromFile = false

		if len(args.EnvVars) > 0 {
			cfg.EnvOverrides = args.EnvVars
		}

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
	if args.Namespace != "" {
		cfg.Namespace = args.Namespace
	}
	if len(args.EnvVars) > 0 {
		if cfg.EnvOverrides == nil {
			cfg.EnvOverrides = make(map[string]string)
		}

		for key, value := range args.EnvVars {
			cfg.EnvOverrides[key] = value
		}
	}

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

func (c *LoadTestConfig) SetEnvOverrides() error {
	// .env filename is fixed and cannot be changed due to security policies enforced via ignore files
	const envFileName = ".env"
	envVars, err := LoadEnvConfig(envFileName)

	if err != nil {
		return err
	}

	if c.EnvOverrides == nil {
		c.EnvOverrides = make(map[string]string)
	}
	c.EnvOverrides = MergeEnvVars(c.EnvOverrides, envVars)

	return nil
}
