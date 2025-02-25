package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rudderlabs/rudder-go-kit/logger"

	"rudder-load/internal/parser"
)

type CLI struct {
	log logger.Logger
}

type CLIArgs struct {
	duration       string
	namespace      string
	loadName       string
	chartFilesPath string
	testFile       string
}

func NewCLI(log logger.Logger) *CLI {
	return &CLI{
		log: log,
	}
}

func (c *CLI) ParseFlags() (*parser.CLIArgs, error) {
	var cli parser.CLIArgs
	flag.StringVar(&cli.Duration, "d", "", "Duration to run (e.g., 1h, 30m, 5s)")
	flag.StringVar(&cli.Namespace, "n", "", "Kubernetes namespace")
	flag.StringVar(&cli.LoadName, "l", "", "Load scenario name")
	flag.StringVar(&cli.ChartFilesPath, "f", "", "Path to the chart files (e.g., artifacts/helm)")
	flag.StringVar(&cli.TestFile, "t", "", "Path to the test file (e.g., tests/spike.test.yaml)")
	flag.Usage = func() {
		c.log.Infon(fmt.Sprintf("Usage: %s [options]", os.Args[0]))
		c.log.Infon("Options:")
		flag.PrintDefaults()
		c.log.Infon("Examples:")
		c.log.Infon(fmt.Sprintf("  %s -t tests/spike.test.yaml    # Runs spike test", os.Args[0]))
	}

	flag.Parse()

	if err := c.ValidateArgs(&cli); err != nil {
		flag.Usage()
		return nil, err
	}

	return &cli, nil
}

func (c *CLI) ValidateArgs(cli *parser.CLIArgs) error {
	if cli.TestFile == "" {
		if cli.Duration == "" || cli.Namespace == "" || cli.LoadName == "" {
			if cli.Duration == "" {
				c.log.Errorn("Error: duration is required")
			}
			if cli.Namespace == "" {
				c.log.Errorn("Error: namespace is required")
			}
			if cli.LoadName == "" {
				c.log.Errorn("Error: load name is required")
			}
			return fmt.Errorf("invalid options")
		}
	}

	return nil
}
