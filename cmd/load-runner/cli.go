package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rudderlabs/rudder-go-kit/logger"
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

func (c *CLI) ParseFlags() (*CLIArgs, error) {
	var cli CLIArgs
	flag.StringVar(&cli.duration, "d", "", "Duration to run (e.g., 1h, 30m, 5s)")
	flag.StringVar(&cli.namespace, "n", "", "Kubernetes namespace")
	flag.StringVar(&cli.loadName, "l", "", "Load scenario name")
	flag.StringVar(&cli.chartFilesPath, "f", "", "Path to the chart files (e.g., artifacts/helm)")
	flag.StringVar(&cli.testFile, "t", "", "Path to the test file (e.g., tests/spike.test.yaml)")
	flag.Usage = func() {
		c.log.Infof("Usage: %s [options]", os.Args[0])
		c.log.Info("Options:")
		flag.PrintDefaults()
		c.log.Info("Examples:")
		c.log.Infof("  %s -t tests/spike.test.yaml    # Runs spike test", os.Args[0])
	}

	flag.Parse()

	// if cli.testFile == "" {
	// 	if cli.duration == "" || cli.namespace == "" || cli.loadName == "" {
	// 		if cli.duration == "" {
	// 			c.log.Error("Error: duration is required")
	// 		}
	// 		if cli.namespace == "" {
	// 			c.log.Error("Error: namespace is required")
	// 		}
	// 		if cli.loadName == "" {
	// 			c.log.Error("Error: load name is required")
	// 		}

	// 		flag.Usage()
	// 		return nil, fmt.Errorf("invalid options")
	// 	}
	// }
	if err := c.ValidateArgs(&cli); err != nil {
		flag.Usage()
		return nil, err
	}

	return &cli, nil
}

func (c *CLI) ValidateArgs(cli *CLIArgs) error {
	if cli.testFile == "" {
		if cli.duration == "" || cli.namespace == "" || cli.loadName == "" {
			if cli.duration == "" {
				c.log.Error("Error: duration is required")
			}
			if cli.namespace == "" {
				c.log.Error("Error: namespace is required")
			}
			if cli.loadName == "" {
				c.log.Error("Error: load name is required")
			}
			return fmt.Errorf("invalid options")
		}
	}

	return nil
}