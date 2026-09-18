// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"fmt"

	"github.com/monvm-dev/monvm/internal/buildvars"
	"github.com/spf13/cobra"
)

// NewRootCommand constructs the complete MonVM command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "monvm",
		Short:         "Lean observability infrastructure",
		SilenceErrors: true,
		Version:       buildvars.BuildVersion(),
	}
	root.SetVersionTemplate(fmt.Sprintf(
		"monvm %s\nbuild date: %s\ncommit: %s\ncommit date: %s\nbranch: %s\n",
		buildvars.BuildVersion(), buildvars.BuildDate(), buildvars.CommitHash(), buildvars.CommitDate(), buildvars.CommitBranch(),
	))
	root.AddCommand(setupCommand(), teardownCommand(), destroyCommand())
	silenceUsageForRuntimeErrors(root)
	return root
}

// silenceUsageForRuntimeErrors suppresses usage output after command execution begins.
func silenceUsageForRuntimeErrors(command *cobra.Command) {
	if command.RunE != nil {
		run := command.RunE
		command.RunE = func(cmd *cobra.Command, args []string) error {
			cmd.Root().SilenceUsage = true
			return run(cmd, args)
		}
	}
	for _, child := range command.Commands() {
		silenceUsageForRuntimeErrors(child)
	}
}
