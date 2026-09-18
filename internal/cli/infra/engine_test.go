// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPrepareWritesEmbeddedModuleAndVariables verifies complete module materialization.
func TestPrepareWritesEmbeddedModuleAndVariables(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	variables := Variables{Name: "demo", Region: "us-east-1", Bucket: "monvm-demo", Mode: "combined", Active: true, IPv4Network: true, MetricsIPv4: true}
	directory, err := Prepare(variables)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"versions.tf", "variables.tf", "network.tf", "compute.tf", "userdata.sh.tftpl", "trust-enroll.sh", "trust-refresh.sh", "monvm.auto.tfvars.json"} {
		if _, err = os.Stat(filepath.Join(directory, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(directory, "monvm.auto.tfvars.json"))
	if err != nil {
		t.Fatal(err)
	}
	var got Variables
	if err = json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != variables.Name || !got.Active || !got.IPv4Network || !got.MetricsIPv4 {
		t.Fatalf("variables = %#v", got)
	}
}

// TestValidName verifies accepted deployment identifier boundaries.
func TestValidName(t *testing.T) {
	for _, name := range []string{"demo", "demo-2", "a"} {
		if !ValidName(name) {
			t.Errorf("ValidName(%q) = false", name)
		}
	}
	for _, name := range []string{"Demo", "2demo", "demo-", ""} {
		if ValidName(name) {
			t.Errorf("ValidName(%q) = true", name)
		}
	}
}
