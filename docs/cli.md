# CLI Reference

## `monvm setup <name>`

Creates or updates a deployment. Important flags:

| Flag | Default | Purpose |
|---|---:|---|
| `--region` | `us-east-1` | AWS region |
| `--vpc-cidr` | `10.73.0.0/24` | Private CIDR used to find or create the VPC |
| `--mode` | `combined` | `combined` or `split` compute |
| `--architecture` | `arm64` | `arm64` or `amd64` |
| `--instance-type` | `t4g.small` | Combined VM size |
| `--metrics-instance-type` | `t4g.nano` | Split metrics VM size |
| `--logs-instance-type` | `t4g.nano` | Split logs VM size |
| `--metrics-ipv4` | false | Allocate a stable public metrics IPv4 address |
| `--logs-ipv4` | false | Allocate a stable public logs IPv4 address |
| `--metrics-volume-size` | `20` | Metrics gp3 size in GiB |
| `--logs-volume-size` | `20` | Logs gp3 size in GiB |
| `--metrics-retention` | `90d` | Metrics retention |
| `--logs-retention` | `14d` | Logs retention |
| `--metrics-allowed-cidr` | none | Repeatable allowed IPv4 or IPv6 CIDR |
| `--logs-allowed-cidr` | none | Repeatable allowed IPv4 or IPv6 CIDR |
| `--metrics-version` | latest | Exact VictoriaMetrics release |
| `--logs-version` | latest | Exact VictoriaLogs release |
| `--envoy-version` | latest | Exact Envoy release |
| `--auto-approve`, `-y` | false | Skip plan confirmation |

`--mode` is optional and defaults to `combined`. AMD64 deployments default to the equivalent `t3` sizes. Combined mode needs three network interfaces, so `small` is the minimum compatible T3/T4g size; each split VM needs only two and can start at `nano`. Re-running `setup` preserves omitted settings, checks for current upstream releases, and applies an authoritative update. Region, bucket, availability zone, and VPC CIDR cannot be changed after creation. Increasing a volume size grows its EBS volume, replaces the attached compute, and expands the ext4 filesystem automatically. Volume sizes cannot be reduced.

The VPC CIDR must be a private `/16` through `/28` and is normalized before it is stored. Zero exact primary-CIDR matches creates a MonVM-owned VPC, one compatible match is reused, and multiple matches fail. A reused VPC must enable DNS support and hostnames, have exactly one Amazon-provided IPv6 `/56`, have exactly one attached internet gateway, and have free subnet ranges. IPv4 space is required only when an IPv4 endpoint is first enabled. MonVM owns only the subnet, route table, security groups, and other resources it adds to a reused VPC.

IPv4 endpoints are independent. Their EIPs remain attached to the persistent service ENIs across compute replacement, teardown, and combined/split transitions. The first enabled IPv4 endpoint performs a one-time replacement of the IPv6-native subnet and service ENIs with dual-stack equivalents; data volumes remain intact, but IPv6 addresses change and downtime occurs. MonVM keeps the subnet dual-stack thereafter. Passing `--metrics-ipv4=false` or `--logs-ipv4=false` removes and releases the corresponding EIP. IPv4 CIDRs are rejected unless that service's IPv4 endpoint is enabled.

## `monvm port-forward <name>`

Uses AWS Systems Manager to forward both loopback-only UIs to the local machine. It prints the URLs after both forwards are ready and runs until interrupted with Ctrl-C:

```sh
monvm port-forward production
```

VictoriaMetrics is available at `http://127.0.0.1:18428` and VictoriaLogs at `http://127.0.0.1:19428`. Use `--metrics-port` or `--logs-port` to select different local ports. The command requires the AWS CLI and AWS Session Manager plugin, uses the deployment's saved region and profile, and binds only to localhost. It works with both combined and split deployments.

## `monvm teardown <name>`

Removes EC2 compute and attachments while preserving service ENIs, stable public IPv6 addresses, enabled EIPs, encrypted data volumes, network resources, bucket, and state.

## `monvm destroy <name>`

Permanently destroys infrastructure, data volumes, and stable addresses, then empties and deletes the deployment bucket and local metadata. The OpenTofu/Terraform plan is the confirmation boundary unless `--auto-approve` is supplied.

## Names and local state

Names start with a lowercase letter, contain only lowercase letters, digits, and hyphens, end in a letter or digit, and are at most 32 characters. Local metadata is stored under the MonVM configuration directory; generated infrastructure and dependency downloads use the MonVM cache directory.
