# MonVM Agent Guide

MonVM is a Go CLI that provisions single-node VictoriaMetrics and VictoriaLogs to AWS using an embedded TF module.

## Layout

- `cmd/monvm` and the root `main.go` are thin executable entrypoints.
- `internal/cli` owns commands, local metadata, dependency resolution, and TF execution.
- `internal/cli/infra/aws` is the embedded AWS root module and VM bootstrap source.
- `internal/cloud/aws` owns direct AWS API access used before and after TF execution.
- `docs` contains website source; `scripts/sitegen` renders ignored output under `dist/website`.
- `scripts/git-hooks` contains the repository's pre-commit and DCO hooks.

## Commands

- `make precommit` — formatting, module format, and focused Go checks.
- `make lint` — Go, rendered user-data, and OpenTofu validation.
- `make test` — race-enabled Go tests.
- `make build` — build `bin/monvm`.
- `make website` — render Markdown documentation into `dist/website`.

## Design constraints

- Preserve separate metrics and logs EBS volumes and stable IPv6 service ENIs in both combined (metrics+logs on one VM) and split modes (separate VMs).
- Find VPCs by exact primary IPv4 CIDR. Reuse one compatible unowned VPC; create and own a VPC only when none matches. Never destroy a reused VPC or its internet gateway.
- `teardown` must retain data, ENIs, network, bucket, and state. `destroy` is the only full deletion path.
- Release archives must be exact open source assets with trusted SHA-256 digests, mirrored to S3, and verified again during boot.
- VictoriaMetrics and VictoriaLogs are loopback-only behind independent Envoy mTLS listeners. Keep ingress deny-by-default.
- Support vertical scaling only. Do not add horizontal or clustered orchestration.
- Keep copyright headers as `Copyright The MonVM Authors`.
- The embedded TF is a generated CLI-owned root module, not a reusable child module; its provider and S3 backend configuration are intentional.
