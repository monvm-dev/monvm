// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"testing"

	"github.com/monvm-dev/monvm/internal/cli/infra"
)

// validOptions returns a minimal valid setup configuration for tests.
func validOptions() setupOptions {
	return setupOptions{region: "us-east-1", mode: "combined", architecture: "arm64", vpcCIDR: "10.73.0.0/24", metricsVolume: 20, logsVolume: 20, metricsRetention: "90d", logsRetention: "14d", metricsCIDRs: []string{"2001:db8::/48"}}
}

// TestValidateSetupOptions verifies rejection of unsupported and inconsistent values.
func TestValidateSetupOptions(t *testing.T) {
	if err := validateSetupOptions(validOptions()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func(*setupOptions)
	}{
		{"mode", func(o *setupOptions) { o.mode = "cluster" }},
		{"architecture", func(o *setupOptions) { o.architecture = "sparc" }},
		{"region", func(o *setupOptions) { o.region = "bad;region" }},
		{"retention", func(o *setupOptions) { o.logsRetention = "forever" }},
		{"IPv4 CIDR without endpoint", func(o *setupOptions) { o.logsCIDRs = []string{"10.0.0.0/8"} }},
		{"unmasked IPv4 CIDR", func(o *setupOptions) { o.logsIPv4 = true; o.logsCIDRs = []string{"192.0.2.1/24"} }},
		{"unmasked IPv6 CIDR", func(o *setupOptions) { o.logsCIDRs = []string{"2001:db8::1/64"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := validOptions()
			test.edit(&o)
			if err := validateSetupOptions(o); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

// TestValidateSetupOptionsAcceptsIPv4CIDRForIPv4Endpoint verifies the IPv4 opt-in path.
func TestValidateSetupOptionsAcceptsIPv4CIDRForIPv4Endpoint(t *testing.T) {
	o := validOptions()
	o.logsIPv4 = true
	o.logsCIDRs = []string{"192.0.2.0/24"}
	if err := validateSetupOptions(o); err != nil {
		t.Fatal(err)
	}
}

// TestNormalizeVPCCIDR verifies masking, private-range enforcement, and prefix boundaries.
func TestNormalizeVPCCIDR(t *testing.T) {
	got, err := normalizeVPCCIDR("10.73.0.42/24")
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.73.0.0/24" {
		t.Fatalf("normalized CIDR = %q, want 10.73.0.0/24", got)
	}
	for _, value := range []string{"10.0.0.0/15", "10.0.0.0/29", "203.0.113.0/24", "fd00::/64", "invalid"} {
		if _, err = normalizeVPCCIDR(value); err == nil {
			t.Errorf("normalizeVPCCIDR(%q) succeeded", value)
		}
	}
}

// TestArchitectureDefaults verifies architecture-compatible instance families.
func TestArchitectureDefaults(t *testing.T) {
	command := setupCommand()
	o := setupOptions{architecture: "amd64"}
	setArchitectureDefaults(command, &o)
	if o.instanceType != "t3.small" || o.metricsType != "t3.nano" || o.logsType != "t3.nano" {
		t.Fatalf("unexpected defaults: %#v", o)
	}
}

// TestSetupDefaults verifies the low-cost initial deployment settings.
func TestSetupDefaults(t *testing.T) {
	command := setupCommand()
	flags := command.Flags()
	want := map[string]string{
		"mode":                  "combined",
		"instance-type":         "t4g.small",
		"metrics-instance-type": "t4g.nano",
		"logs-instance-type":    "t4g.nano",
		"metrics-ipv4":          "false",
		"logs-ipv4":             "false",
		"metrics-volume-size":   "20",
		"logs-volume-size":      "20",
	}
	for name, expected := range want {
		if actual := flags.Lookup(name).DefValue; actual != expected {
			t.Errorf("%s default = %q, want %q", name, actual, expected)
		}
	}
}

// TestPreserveSetupOptionsRejectsVolumeShrink verifies that persistent volumes only grow.
func TestPreserveSetupOptionsRejectsVolumeShrink(t *testing.T) {
	command := setupCommand()
	if err := command.Flags().Set("metrics-volume-size", "19"); err != nil {
		t.Fatal(err)
	}
	o := validOptions()
	o.metricsVolume = 19
	prior := infra.Variables{
		Region: "us-east-1", Mode: "combined", Architecture: "arm64",
		CombinedInstanceType: "t4g.small", MetricsInstanceType: "t4g.nano", LogsInstanceType: "t4g.nano",
		MetricsVolumeSize: 20, LogsVolumeSize: 20, MetricsRetention: "90d", LogsRetention: "14d",
	}
	if err := preserveSetupOptions(command, &o, prior); err == nil {
		t.Fatal("expected volume shrink to be rejected")
	}
}

// TestPreserveSetupOptionsRetainsIPv4Endpoints verifies omitted flags retain endpoint state.
func TestPreserveSetupOptionsRetainsIPv4Endpoints(t *testing.T) {
	command := setupCommand()
	o := validOptions()
	prior := infra.Variables{
		Region: "us-east-1", Mode: "combined", Architecture: "arm64",
		CombinedInstanceType: "t4g.small", MetricsInstanceType: "t4g.nano", LogsInstanceType: "t4g.nano",
		MetricsVolumeSize: 20, LogsVolumeSize: 20, MetricsRetention: "90d", LogsRetention: "14d",
		IPv4Network: true, MetricsIPv4: true, LogsIPv4: true,
	}
	if err := preserveSetupOptions(command, &o, prior); err != nil {
		t.Fatal(err)
	}
	if !o.ipv4Network || !o.metricsIPv4 || !o.logsIPv4 {
		t.Fatalf("IPv4 endpoint settings were not preserved: %#v", o)
	}
}

// TestPreserveSetupOptionsKeepsDualStackAfterIPv4EndpointsAreDisabled verifies network migration remains sticky.
func TestPreserveSetupOptionsKeepsDualStackAfterIPv4EndpointsAreDisabled(t *testing.T) {
	command := setupCommand()
	for _, name := range []string{"metrics-ipv4", "logs-ipv4"} {
		if err := command.Flags().Set(name, "false"); err != nil {
			t.Fatal(err)
		}
	}
	o := validOptions()
	prior := infra.Variables{
		Region: "us-east-1", Mode: "combined", Architecture: "arm64",
		CombinedInstanceType: "t4g.small", MetricsInstanceType: "t4g.nano", LogsInstanceType: "t4g.nano",
		MetricsVolumeSize: 20, LogsVolumeSize: 20, MetricsRetention: "90d", LogsRetention: "14d",
		IPv4Network: true, MetricsIPv4: true, LogsIPv4: true,
	}
	if err := preserveSetupOptions(command, &o, prior); err != nil {
		t.Fatal(err)
	}
	if !o.ipv4Network || o.metricsIPv4 || o.logsIPv4 {
		t.Fatalf("unexpected IPv4 endpoint settings: %#v", o)
	}
}
