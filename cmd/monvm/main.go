// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/monvm-dev/monvm/internal/cli"
)

// main runs the MonVM CLI and exits with its status code.
func main() {
	os.Exit(cli.Execute())
}
