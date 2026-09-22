// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"github.com/monvm-dev/monvm/internal/portforward"
	"github.com/spf13/cobra"
)

// portForwardCommand constructs the command that exposes both private UIs locally.
func portForwardCommand() *cobra.Command {
	metricsPort := portforward.DefaultMetricsPort
	logsPort := portforward.DefaultLogsPort
	command := &cobra.Command{
		Use:   "port-forward <name>",
		Short: "Forward the metrics and logs UIs to localhost",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			_, variables, err := loadDeployment(args[0])
			if err != nil {
				return err
			}
			return portforward.Run(command.Context(), portforward.Config{
				Name:        variables.Name,
				Region:      variables.Region,
				Profile:     variables.Profile,
				Mode:        variables.Mode,
				Active:      variables.Active,
				MetricsPort: metricsPort,
				LogsPort:    logsPort,
			}, command.OutOrStdout(), command.ErrOrStderr())
		},
	}
	command.Flags().IntVar(&metricsPort, "metrics-port", portforward.DefaultMetricsPort, "local port for the VictoriaMetrics UI")
	command.Flags().IntVar(&logsPort, "logs-port", portforward.DefaultLogsPort, "local port for the VictoriaLogs UI")
	return command
}
