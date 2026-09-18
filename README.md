# MonVM — Lean Metrics and Logs on One VM

MonVM provisions a deliberately small, vertically-scaled observability stack on AWS. It runs the open source single-node editions of [VictoriaMetrics](https://github.com/VictoriaMetrics/VictoriaMetrics) and [VictoriaLogs](https://github.com/VictoriaMetrics/VictoriaLogs) on one EC2 instance, or one instance per service.

The CLI generates an inspectable TF module, mirrors verified VictoriaMetrics, VictoriaLogs, and Envoy releases into your S3 bucket, and lets cloud-init/systemd do the rest.

## Goals

- Low fixed cost: EC2, encrypted EBS, S3, and ordinary AWS networking.
- Low maintenance: three upstream binaries managed by systemd on Debian.
- Stable endpoints while replacing or resizing compute.
- A clean path from one combined VM to separate metrics and logs VMs.
- Safe reuse of a compatible existing VPC without taking ownership of it.
- Infrastructure you can inspect and operate with standard AWS and OpenTofu/Terraform tools.

## Quick Start

Install OpenTofu/Terraform, configure AWS credentials, then run:

```sh
monvm setup production \
  --region us-east-1 \
  --metrics-allowed-cidr 2001:db8:100::/48 \
  --logs-allowed-cidr 2001:db8:100::/48
```

MonVM defaults to the smallest instance that supports combined mode's three network interfaces: an ARM64 `t4g.small`. Metrics and logs each start on an independent 20 GiB encrypted gp3 volume. Stable public IPv4 addresses are optional and disabled by default. No ingress is allowed when the corresponding CIDR flag is omitted.

To move to independent VMs without moving data or changing service IPv6 addresses:

```sh
monvm setup production --mode split
```

To remove compute while retaining the network, service addresses, S3 state, and data volumes:

```sh
monvm teardown production
```

To permanently remove everything:

```sh
monvm destroy production
```

## Important Limits

MonVM is single-node software by design. It does not provide high availability or horizontal scaling. Mode changes and instance replacements cause downtime. VictoriaMetrics and VictoriaLogs are loopback-only, each behind an Envoy TLS 1.3 listener that requires a client certificate issued by a CA in the generic S3 client CA store.

Capacity depends heavily on label cardinality, query patterns, log structure, retention, and compression. Benchmark your workload before choosing an instance or promising an ingestion rate. See [Capacity and scaling](./docs/capacity.md).

## Documentation

- [Getting started](./docs/getting-started.md)
- [Architecture and infrastructure](./docs/architecture.md)
- [CLI reference](./docs/cli.md)
- [Capacity and scaling](./docs/capacity.md)
- [Operations and security](./docs/operations.md)

## Development

Run `make setup`, then `make precommit lint test build website`.

## Releases

Immutable semantic-version tags publish signed, checksummed release archives, SBOMs, provenance, and the Homebrew formula through GoReleaser.

## License

MonVM is licensed under the Apache License, Version 2.0. Copyright The MonVM Authors. See [LICENSE](./LICENSE).
