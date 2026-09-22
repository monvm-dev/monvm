// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package infra

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
)

// namePattern recognizes valid deployment identifiers.
var namePattern = regexp.MustCompile(`^[a-z]([a-z0-9-]{0,30}[a-z0-9])?$`)

// Artifact contains the immutable S3 location and verification data for a release artifact.
type Artifact struct {
	ObjectKey string `json:"object_key"`
	SHA256    string `json:"sha256"`
	Binary    string `json:"binary"`
}

// Variables is the complete input contract for the embedded TF root module.
type Variables struct {
	Name                 string   `json:"name"`
	Region               string   `json:"region"`
	Profile              string   `json:"profile"`
	Bucket               string   `json:"bucket"`
	VPCCIDR              string   `json:"vpc_cidr"`
	Zone                 string   `json:"zone"`
	Mode                 string   `json:"mode"`
	Active               bool     `json:"active"`
	Architecture         string   `json:"architecture"`
	CombinedInstanceType string   `json:"combined_instance_type"`
	MetricsInstanceType  string   `json:"metrics_instance_type"`
	LogsInstanceType     string   `json:"logs_instance_type"`
	MetricsVolumeSize    int      `json:"metrics_volume_size"`
	LogsVolumeSize       int      `json:"logs_volume_size"`
	MetricsRetention     string   `json:"metrics_retention"`
	LogsRetention        string   `json:"logs_retention"`
	IPv4Network          bool     `json:"ipv4_network"`
	MetricsIPv4          bool     `json:"metrics_ipv4"`
	LogsIPv4             bool     `json:"logs_ipv4"`
	MetricsAllowedCIDRs  []string `json:"metrics_allowed_cidrs"`
	LogsAllowedCIDRs     []string `json:"logs_allowed_cidrs"`
	Metrics              Artifact `json:"metrics_artifact"`
	Logs                 Artifact `json:"logs_artifact"`
	Envoy                Artifact `json:"envoy_artifact"`
}

// ValidName reports whether name is a valid deployment identifier.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// SelectCommand locates the configured OpenTofu/Terraform executable, preferring OpenTofu.
func SelectCommand() (string, error) {
	if override := os.Getenv("MONVM_TF_CMD"); override != "" {
		path, err := exec.LookPath(override)
		if err != nil {
			return "", fmt.Errorf("OpenTofu/Terraform command %q: %w", override, err)
		}
		return path, nil
	}
	for _, name := range []string{"tofu", "terraform"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", errors.New("OpenTofu/Terraform is required (install tofu or terraform)")
}
