// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"

	"github.com/monvm-dev/monvm/internal/cli/infra"
	"github.com/monvm-dev/monvm/internal/cloud/aws"
	"github.com/spf13/cobra"
)

// teardownCommand constructs the command that removes compute while retaining persistent resources.
func teardownCommand() *cobra.Command {
	var autoApprove bool
	command := &cobra.Command{Use: "teardown <name>", Short: "Remove compute while retaining data and stable addresses", Args: cobra.ExactArgs(1)}
	command.RunE = func(command *cobra.Command, args []string) error {
		store, variables, err := loadDeployment(args[0])
		if err != nil {
			return err
		}
		variables.Active = false
		if err = infra.Run(command.Context(), variables, false, autoApprove, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr()); err != nil {
			return err
		}
		deployment, body, err := deploymentBody(variables)
		if err != nil {
			return err
		}
		remote, err := aws.New(command.Context(), variables.Region, variables.Profile, variables.Bucket)
		if err != nil {
			return err
		}
		if err = remote.Put(command.Context(), "deployment.json", bytes.NewReader(body), int64(len(body))); err != nil {
			return err
		}
		return store.Save(deployment)
	}
	command.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "skip confirmation prompts")
	return command
}
