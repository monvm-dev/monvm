// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/monvm-dev/monvm/internal/cli/infra"
	"github.com/monvm-dev/monvm/internal/cloud/aws"
	"github.com/spf13/cobra"
)

// destroyCommand constructs the command that permanently removes a deployment.
func destroyCommand() *cobra.Command {
	var autoApprove bool
	command := &cobra.Command{Use: "destroy <name>", Short: "Permanently destroy a MonVM deployment", Args: cobra.ExactArgs(1)}
	command.RunE = func(command *cobra.Command, args []string) error {
		store, variables, err := loadDeployment(args[0])
		if err != nil {
			return err
		}
		if err = infra.Run(command.Context(), variables, true, autoApprove, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr()); err != nil {
			return err
		}
		remote, err := aws.New(command.Context(), variables.Region, variables.Profile, variables.Bucket)
		if err != nil {
			return err
		}
		if err = remote.EmptyAndDelete(command.Context()); err != nil {
			return fmt.Errorf("delete bucket %q: %w", variables.Bucket, err)
		}
		if err = infra.Clean(args[0]); err != nil {
			return err
		}
		return store.Delete(args[0])
	}
	command.Flags().BoolVarP(&autoApprove, "auto-approve", "y", false, "skip confirmation prompts")
	return command
}
