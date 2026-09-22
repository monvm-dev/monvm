// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package portforward

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// instances identifies the host for each MonVM service.
type instances struct {
	metrics string
	logs    string
}

// findInstances discovers the running instance for each service by deployment tags.
func findInstances(ctx context.Context, aws string, config Config) (instances, error) {
	var roles []string
	switch config.Mode {
	case "combined":
		roles = []string{"combined"}
	case "split":
		roles = []string{"metrics", "logs"}
	default:
		return instances{}, fmt.Errorf("unsupported deployment mode %q", config.Mode)
	}
	ids := make(map[string]string, len(roles))
	for _, role := range roles {
		arguments := awsArguments(config, "ec2", "describe-instances",
			"--filters",
			"Name=instance-state-name,Values=running",
			"Name=tag:monvm:deployment,Values="+config.Name,
			"Name=tag:Name,Values="+config.Name+"-"+role,
			"--query", "Reservations[].Instances[].InstanceId",
			"--output", "text",
		)
		output, err := exec.CommandContext(ctx, aws, arguments...).Output()
		if err != nil {
			return instances{}, fmt.Errorf("find %s instance: %w", role, commandError(err))
		}
		matches := strings.Fields(string(output))
		if len(matches) != 1 {
			return instances{}, fmt.Errorf("expected one running %s instance, found %d", role, len(matches))
		}
		ids[role] = matches[0]
	}
	if config.Mode == "combined" {
		return instances{metrics: ids["combined"], logs: ids["combined"]}, nil
	}
	return instances{metrics: ids["metrics"], logs: ids["logs"]}, nil
}

// awsArguments prefixes an AWS CLI invocation with the deployment's location and profile.
func awsArguments(config Config, arguments ...string) []string {
	result := []string{"--region", config.Region}
	if config.Profile != "" {
		result = append(result, "--profile", config.Profile)
	}
	return append(result, arguments...)
}

// commandError extracts stderr from a failed external command when available.
func commandError(err error) error {
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && len(exitError.Stderr) > 0 {
		return errors.New(strings.TrimSpace(string(exitError.Stderr)))
	}
	return err
}
