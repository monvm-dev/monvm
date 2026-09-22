// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import "testing"

// TestPortForwardDefaults verifies the documented local UI ports.
func TestPortForwardDefaults(t *testing.T) {
	flags := portForwardCommand().Flags()
	if got := flags.Lookup("metrics-port").DefValue; got != "18428" {
		t.Errorf("metrics port = %s, want 18428", got)
	}
	if got := flags.Lookup("logs-port").DefValue; got != "19428" {
		t.Errorf("logs port = %s, want 19428", got)
	}
}
