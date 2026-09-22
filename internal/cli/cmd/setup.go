// MonVM <https://monvm.dev>
// Copyright The MonVM Authors
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"

	"github.com/monvm-dev/monvm/internal/cli/config"
	"github.com/monvm-dev/monvm/internal/cli/dependencies"
	"github.com/monvm-dev/monvm/internal/cli/infra"
	"github.com/monvm-dev/monvm/internal/cloud/aws"
	"github.com/spf13/cobra"
)

// setupOptions contains the user-selected and persisted setup configuration.
type setupOptions struct {
	region, profile, bucket, zone, mode, architecture string
	instanceType, metricsType, logsType               string
	metricsVersion, logsVersion, envoyVersion         string
	metricsRetention, logsRetention                   string
	vpcCIDR                                           string
	metricsCIDRs, logsCIDRs                           []string
	metricsVolume, logsVolume                         int
	ipv4Network, metricsIPv4, logsIPv4, autoApprove   bool
}

// serviceEndpoint contains one service's address-family and ingress configuration.
type serviceEndpoint struct {
	name  string
	ipv4  bool
	cidrs []string
}

// Input validation patterns shared by setup options.
var (
	awsRegionPattern = regexp.MustCompile(`^[a-z]{2}(-gov)?-[a-z]+-[0-9]+$`)
	bucketPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	retentionPattern = regexp.MustCompile(`^[1-9][0-9]*(h|d|w|y)$`)
)

// setupCommand constructs the command that creates or updates a deployment.
func setupCommand() *cobra.Command {
	o := setupOptions{}
	command := &cobra.Command{
		Use:   "setup <name>",
		Short: "Create or update a MonVM deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return runSetup(command, args[0], o)
		},
	}
	flags := command.Flags()
	flags.StringVar(&o.region, "region", "us-east-1", "AWS region")
	flags.StringVar(&o.profile, "profile", "", "AWS shared configuration profile")
	flags.StringVar(&o.bucket, "bucket", "", "S3 bucket (generated when omitted)")
	flags.StringVar(&o.zone, "zone", "", "availability zone (first available when omitted)")
	flags.StringVar(&o.mode, "mode", "combined", "compute layout: combined or split")
	flags.StringVar(&o.architecture, "architecture", "arm64", "instance architecture: arm64 or amd64")
	flags.StringVar(&o.instanceType, "instance-type", "t4g.small", "combined mode EC2 instance type")
	flags.StringVar(&o.metricsType, "metrics-instance-type", "t4g.nano", "split mode metrics EC2 instance type")
	flags.StringVar(&o.logsType, "logs-instance-type", "t4g.nano", "split mode logs EC2 instance type")
	flags.IntVar(&o.metricsVolume, "metrics-volume-size", 20, "metrics EBS volume size in GiB")
	flags.IntVar(&o.logsVolume, "logs-volume-size", 20, "logs EBS volume size in GiB")
	flags.StringVar(&o.metricsRetention, "metrics-retention", "90d", "VictoriaMetrics retention period")
	flags.StringVar(&o.logsRetention, "logs-retention", "14d", "VictoriaLogs retention period")
	flags.StringVar(&o.metricsVersion, "metrics-version", "", "VictoriaMetrics version (latest when omitted)")
	flags.StringVar(&o.logsVersion, "logs-version", "", "VictoriaLogs version (latest when omitted)")
	flags.StringVar(&o.envoyVersion, "envoy-version", "", "Envoy version (latest when omitted)")
	flags.BoolVar(&o.metricsIPv4, "metrics-ipv4", false, "allocate a stable public IPv4 address for metrics")
	flags.BoolVar(&o.logsIPv4, "logs-ipv4", false, "allocate a stable public IPv4 address for logs")
	flags.StringSliceVar(&o.metricsCIDRs, "metrics-allowed-cidr", nil, "IPv4 or IPv6 CIDR allowed to reach metrics (repeatable)")
	flags.StringSliceVar(&o.logsCIDRs, "logs-allowed-cidr", nil, "IPv4 or IPv6 CIDR allowed to reach logs (repeatable)")
	flags.StringVar(&o.vpcCIDR, "vpc-cidr", "10.73.0.0/24", "VPC IPv4 CIDR used for the VPC control plane")
	flags.BoolVarP(&o.autoApprove, "auto-approve", "y", false, "skip confirmation prompts")
	return command
}

// runSetup resolves dependencies, persists metadata, and applies the requested infrastructure.
func runSetup(command *cobra.Command, name string, o setupOptions) error {
	if !infra.ValidName(name) {
		return fmt.Errorf("invalid deployment name %q", name)
	}
	store, err := config.DefaultStore()
	if err != nil {
		return err
	}
	previous, loadErr := store.Load(name)
	if loadErr == nil {
		var prior infra.Variables
		if err = json.Unmarshal(previous.Variables, &prior); err != nil {
			return fmt.Errorf("read previous deployment variables: %w", err)
		}
		if err = preserveSetupOptions(command, &o, prior); err != nil {
			return err
		}
	} else if !errors.Is(loadErr, config.ErrNotFound) {
		return loadErr
	} else if o.architecture == "amd64" {
		setArchitectureDefaults(command, &o)
	}
	o.vpcCIDR, err = normalizeVPCCIDR(o.vpcCIDR)
	if err != nil {
		return err
	}
	if err = validateSetupOptions(o); err != nil {
		return err
	}
	o.ipv4Network = o.ipv4Network || o.metricsIPv4 || o.logsIPv4
	if o.bucket == "" {
		o.bucket, err = generatedBucket(name)
		if err != nil {
			return err
		}
	}
	cache, err := config.CacheDir()
	if err != nil {
		return err
	}
	resolver := dependencies.Resolver{}
	metrics, err := resolver.Resolve(command.Context(), dependencies.VictoriaMetrics, o.metricsVersion, o.architecture)
	if err != nil {
		return err
	}
	logs, err := resolver.Resolve(command.Context(), dependencies.VictoriaLogs, o.logsVersion, o.architecture)
	if err != nil {
		return err
	}
	envoy, err := resolver.Resolve(command.Context(), dependencies.Envoy, o.envoyVersion, o.architecture)
	if err != nil {
		return err
	}
	dependencyCache := filepath.Join(cache, "dependencies")
	metrics, err = dependencies.Download(command.Context(), nil, dependencyCache, metrics)
	if err != nil {
		return err
	}
	logs, err = dependencies.Download(command.Context(), nil, dependencyCache, logs)
	if err != nil {
		return err
	}
	envoy, err = dependencies.Download(command.Context(), nil, dependencyCache, envoy)
	if err != nil {
		return err
	}
	variables := infra.Variables{
		Name: name, Region: o.region, Profile: o.profile, Bucket: o.bucket, VPCCIDR: o.vpcCIDR, Zone: o.zone,
		Mode: o.mode, Active: true, Architecture: o.architecture, CombinedInstanceType: o.instanceType,
		MetricsInstanceType: o.metricsType, LogsInstanceType: o.logsType, MetricsVolumeSize: o.metricsVolume,
		LogsVolumeSize: o.logsVolume, MetricsRetention: o.metricsRetention, LogsRetention: o.logsRetention,
		IPv4Network: o.ipv4Network, MetricsIPv4: o.metricsIPv4, LogsIPv4: o.logsIPv4,
		MetricsAllowedCIDRs: o.metricsCIDRs, LogsAllowedCIDRs: o.logsCIDRs,
		Metrics: infra.Artifact{ObjectKey: metrics.ObjectKey, SHA256: metrics.SHA256, Binary: metrics.Product.Binary},
		Logs:    infra.Artifact{ObjectKey: logs.ObjectKey, SHA256: logs.SHA256, Binary: logs.Product.Binary},
		Envoy:   infra.Artifact{ObjectKey: envoy.ObjectKey, SHA256: envoy.SHA256, Binary: envoy.Name},
	}
	remote, err := aws.New(command.Context(), o.region, o.profile, o.bucket)
	if err != nil {
		return err
	}
	if err = remote.Ensure(command.Context()); err != nil {
		return err
	}
	for _, artifact := range []dependencies.Artifact{metrics, logs, envoy} {
		file, openErr := os.Open(artifact.Path)
		if openErr != nil {
			return openErr
		}
		info, statErr := file.Stat()
		if statErr == nil {
			statErr = remote.Put(command.Context(), artifact.ObjectKey, file, info.Size())
		}
		closeErr := file.Close()
		if statErr != nil {
			return statErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	deployment, body, err := deploymentBody(variables)
	if err != nil {
		return err
	}
	if err = remote.Put(command.Context(), "deployment.json", bytes.NewReader(body), int64(len(body))); err != nil {
		return err
	}
	if err = store.Save(deployment); err != nil {
		return err
	}
	if len(o.metricsCIDRs) == 0 || len(o.logsCIDRs) == 0 {
		_, _ = fmt.Fprintln(command.ErrOrStderr(), "Warning: endpoints without an allowed CIDR will not accept traffic.")
	}
	for _, endpoint := range serviceEndpoints(o) {
		allowed := false
		for _, cidr := range endpoint.cidrs {
			prefix, _ := netip.ParsePrefix(cidr)
			allowed = allowed || prefix.Addr().Is4()
		}
		if endpoint.ipv4 && !allowed {
			_, _ = fmt.Fprintf(command.ErrOrStderr(), "Warning: the %s IPv4 endpoint has no allowed IPv4 CIDR.\n", endpoint.name)
		}
	}
	return infra.Run(command.Context(), variables, false, o.autoApprove, command.InOrStdin(), command.OutOrStdout(), command.ErrOrStderr())
}

// preserveSetupOptions merges omitted flags with immutable and persisted deployment settings.
func preserveSetupOptions(command *cobra.Command, o *setupOptions, prior infra.Variables) error {
	if command.Flags().Changed("region") && o.region != prior.Region {
		return errors.New("region cannot be changed for an existing deployment")
	}
	if command.Flags().Changed("bucket") && o.bucket != prior.Bucket {
		return errors.New("bucket cannot be changed for an existing deployment")
	}
	if command.Flags().Changed("zone") && o.zone != prior.Zone {
		return errors.New("availability zone cannot be changed because the data volumes are zone-bound")
	}
	if command.Flags().Changed("vpc-cidr") && o.vpcCIDR != prior.VPCCIDR {
		return errors.New("VPC CIDR cannot be changed for an existing deployment")
	}
	o.region = prior.Region
	o.bucket = prior.Bucket
	o.zone = prior.Zone
	o.vpcCIDR = prior.VPCCIDR
	if !command.Flags().Changed("profile") {
		o.profile = prior.Profile
	}
	if !command.Flags().Changed("mode") {
		o.mode = prior.Mode
	}
	architectureChanged := command.Flags().Changed("architecture") && o.architecture != prior.Architecture
	if !command.Flags().Changed("architecture") {
		o.architecture = prior.Architecture
	}
	if !command.Flags().Changed("instance-type") {
		o.instanceType = prior.CombinedInstanceType
	}
	if !command.Flags().Changed("metrics-instance-type") {
		o.metricsType = prior.MetricsInstanceType
	}
	if !command.Flags().Changed("logs-instance-type") {
		o.logsType = prior.LogsInstanceType
	}
	if architectureChanged {
		setArchitectureDefaults(command, o)
	}
	if !command.Flags().Changed("metrics-volume-size") {
		o.metricsVolume = prior.MetricsVolumeSize
	}
	if !command.Flags().Changed("logs-volume-size") {
		o.logsVolume = prior.LogsVolumeSize
	}
	if o.metricsVolume < prior.MetricsVolumeSize || o.logsVolume < prior.LogsVolumeSize {
		return errors.New("EBS volume sizes cannot be reduced")
	}
	if !command.Flags().Changed("metrics-retention") {
		o.metricsRetention = prior.MetricsRetention
	}
	if !command.Flags().Changed("logs-retention") {
		o.logsRetention = prior.LogsRetention
	}
	o.ipv4Network = prior.IPv4Network
	if !command.Flags().Changed("metrics-ipv4") {
		o.metricsIPv4 = prior.MetricsIPv4
	}
	if !command.Flags().Changed("logs-ipv4") {
		o.logsIPv4 = prior.LogsIPv4
	}
	if !command.Flags().Changed("metrics-allowed-cidr") {
		o.metricsCIDRs = prior.MetricsAllowedCIDRs
	}
	if !command.Flags().Changed("logs-allowed-cidr") {
		o.logsCIDRs = prior.LogsAllowedCIDRs
	}
	return nil
}

// setArchitectureDefaults selects compatible T3 or T4g instance defaults.
func setArchitectureDefaults(command *cobra.Command, o *setupOptions) {
	prefix := "t4g"
	if o.architecture == "amd64" {
		prefix = "t3"
	}
	if !command.Flags().Changed("instance-type") {
		o.instanceType = prefix + ".small"
	}
	if !command.Flags().Changed("metrics-instance-type") {
		o.metricsType = prefix + ".nano"
	}
	if !command.Flags().Changed("logs-instance-type") {
		o.logsType = prefix + ".nano"
	}
}

// validateSetupOptions rejects unsupported or inconsistent setup values.
func validateSetupOptions(o setupOptions) error {
	if o.mode != "combined" && o.mode != "split" {
		return fmt.Errorf("invalid mode %q: use combined or split", o.mode)
	}
	if o.architecture != "arm64" && o.architecture != "amd64" {
		return fmt.Errorf("invalid architecture %q: use arm64 or amd64", o.architecture)
	}
	if !awsRegionPattern.MatchString(o.region) {
		return fmt.Errorf("invalid AWS region %q", o.region)
	}
	if o.bucket != "" && !bucketPattern.MatchString(o.bucket) {
		return fmt.Errorf("invalid S3 bucket %q", o.bucket)
	}
	if o.metricsVolume < 1 || o.logsVolume < 1 {
		return errors.New("volume sizes must be at least 1 GiB")
	}
	if !retentionPattern.MatchString(o.metricsRetention) || !retentionPattern.MatchString(o.logsRetention) {
		return errors.New("retention periods must be a positive number followed by h, d, w, or y")
	}
	for _, endpoint := range serviceEndpoints(o) {
		for _, cidr := range endpoint.cidrs {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil || prefix.Addr().Is4In6() || prefix != prefix.Masked() {
				return fmt.Errorf("invalid %s CIDR %q", endpoint.name, cidr)
			}
			if prefix.Addr().Is4() && !endpoint.ipv4 {
				return fmt.Errorf("%s IPv4 CIDR %q requires --%s-ipv4", endpoint.name, cidr, endpoint.name)
			}
		}
	}
	return nil
}

// serviceEndpoints returns the metrics and logs endpoint settings for validation and warnings.
func serviceEndpoints(o setupOptions) []serviceEndpoint {
	return []serviceEndpoint{
		{name: "metrics", ipv4: o.metricsIPv4, cidrs: o.metricsCIDRs},
		{name: "logs", ipv4: o.logsIPv4, cidrs: o.logsCIDRs},
	}
}

// normalizeVPCCIDR validates a private VPC range and returns its masked form.
func normalizeVPCCIDR(value string) (string, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() || !prefix.Addr().IsPrivate() || prefix.Bits() < 16 || prefix.Bits() > 28 {
		return "", errors.New("VPC CIDR must be a private IPv4 CIDR between /16 and /28")
	}
	return prefix.Masked().String(), nil
}

// generatedBucket returns a deployment-scoped bucket name with a random suffix.
func generatedBucket(name string) (string, error) {
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "monvm-" + name + "-" + hex.EncodeToString(random), nil
}
