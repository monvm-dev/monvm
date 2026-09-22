# Getting Started

## Install

For macOS and Linux users with Homebrew:

```sh
brew install monvm-dev/tap/monvm
```

Otherwise install with Go:

```bash
go install github.com/monvm-dev/monvm@latest
```

## Prerequisites

- AWS credentials with permission to manage EC2, VPC, EBS, IAM, and one S3 bucket.
- OpenTofu/Terraform 1.11+.
- IPv6 connectivity from every client that will ingest or query data, unless its service uses the optional IPv4 endpoint.

MonVM selects OpenTofu first and falls back to Terraform. Set `MONVM_TF_CMD` to choose an explicit executable.

Local UI access additionally requires the [AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) and [AWS Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html).

## Create a deployment

```sh
monvm setup production \
  --region us-east-1 \
  --metrics-allowed-cidr 2001:db8:100::/48 \
  --logs-allowed-cidr 2001:db8:100::/48
```

`setup` resolves exact open-source VictoriaMetrics, VictoriaLogs, and Envoy releases, requires GitHub's SHA-256 digest, downloads and verifies each artifact, uploads all three to a private S3 bucket, and applies the generated infrastructure plan. The VM downloads only these S3 mirrors during boot.

Review every plan before accepting it. Use `--auto-approve` only in automation.

`--vpc-cidr` accepts a private `/16` through `/28` and selects networking by exact primary IPv4 CIDR. If no VPC matches, MonVM creates and owns one. If one VPC matches, MonVM reuses it without managing its lifecycle after confirming that DNS support and hostnames are enabled, it has one Amazon-provided IPv6 `/56`, one attached internet gateway, and free subnet ranges. IPv4 space is required only when an IPv4 endpoint is first enabled. Multiple matching VPCs fail rather than selecting one ambiguously.

## Choose a layout

Combined mode is the default and runs both services on one VM:

```sh
monvm setup production --instance-type t4g.small
```

Split mode uses a dedicated VM for each service:

```sh
monvm setup production --mode split \
  --metrics-instance-type t4g.nano \
  --logs-instance-type t4g.nano
```

Combined mode defaults to `t4g.small`, the smallest T4g size that supports its management, metrics, and logs network interfaces. Each split VM defaults to `t4g.nano` because it needs only a management and one service interface. These are starting points; monitor the deployment and increase the instance types as its workload grows.

The metrics and logs volumes and service ENIs are always independent. Each volume starts at 20 GiB. Switching modes reattaches them to replacement compute. It does not copy data or change endpoint addresses, but it does cause downtime.

Grow either volume by re-running `setup` with a larger size:

```sh
monvm setup production \
  --metrics-volume-size 40 \
  --logs-volume-size 60
```

MonVM grows the EBS volumes, replaces the attached compute, and expands each ext4 filesystem during boot. Data and stable service addresses are retained, but the replacement causes downtime. EBS volumes cannot shrink, and MonVM rejects a requested reduction.

## Send data

The TF outputs report stable IPv6 HTTPS URLs for VictoriaMetrics on port 8428 and VictoriaLogs on port 9428. Use VictoriaMetrics-compatible Prometheus remote write and a VictoriaLogs-supported ingestion protocol. Consult the upstream projects for endpoint-specific client configuration.

Both Victoria services bind only to loopback. Independent Envoy listeners own the external metrics and logs endpoints and require TLS 1.3 with a trusted client certificate in addition to the configured CIDR boundary.

## Access the UIs

Forward both private UIs to localhost through AWS Systems Manager:

```sh
monvm port-forward production
```

Once both sessions are ready, the command prints the VictoriaMetrics and VictoriaLogs localhost URLs. Keep it running while using the UIs and press Ctrl-C to stop. No public ingress or client certificate is needed for these local forwards because each Systems Manager session connects directly to the service's loopback port on its instance.

### Optional IPv4 endpoints

Allocate a stable public IPv4 address for either service independently:

```sh
monvm setup production \
  --metrics-ipv4 \
  --metrics-allowed-cidr 198.51.100.0/24 \
  --logs-ipv4 \
  --logs-allowed-cidr 203.0.113.0/24
```

`metrics_ipv4_endpoint` and `logs_ipv4_endpoint` are emitted only for enabled services. Each address is an EIP associated with the persistent service ENI, so it survives compute replacement, teardown, and combined/split transitions. Disabling a service's IPv4 option releases its EIP. Enabling IPv4 on an existing service regenerates its endpoint certificate with the public IPv4 address in the SAN. The certificate remains signed by the persistent service CA, so clients do not need a new trust anchor.

The first IPv4 endpoint converts the persistent service subnet from IPv6-native to dual-stack. This one-time change replaces the subnet and service ENIs, changes their IPv6 addresses, and causes downtime; the EBS data volumes remain intact. MonVM keeps the subnet dual-stack thereafter, so disabling or re-enabling EIPs does not repeat that network migration.

The existing `--metrics-allowed-cidr` and `--logs-allowed-cidr` flags accept both IPv4 and IPv6 CIDRs. An IPv4 CIDR requires the corresponding IPv4 option. Ingress remains deny-by-default for both address families.

## Populate the client CA store

Each client source publishes a PEM bundle containing one or more CA certificates to one exact object:

```text
s3://YOUR_MONVM_BUCKET/trust/SOURCE_ID.pem
```

`SOURCE_ID` must be a lowercase DNS-style identifier of at most 63 characters. The bundle must contain only CA certificates—never private keys. A publisher needs only `s3:PutObject` on its exact object; it does not need list, read, or delete access. For example:

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Action": "s3:PutObject",
    "Resource": "arn:aws:s3:::YOUR_MONVM_BUCKET/trust/SOURCE_ID.pem"
  }]
}
```

Each service independently lists the client CA store once at boot and enrolls matching objects under the `trust/` prefix. A combined VM therefore performs one boot-time listing for metrics and one for logs. Each refuses a truncated listing, so the client CA store is intentionally limited to 1,000 total objects. New source IDs require a VM reboot or replacement; already enrolled objects are fetched every 10 minutes. Valid replacements reload in Envoy without a restart. A missing object revokes that source, while a transient S3 error or malformed replacement retains its last-known-good bundle.

For uninterrupted CA rotation, publish a bundle containing both the old and new CA, wait for MonVM to refresh it, rotate clients, then publish a bundle containing only the new CA.

MonVM generates a private CA and CA-signed endpoint certificate on each persistent service volume. It publishes the CA certificates at `s3://YOUR_MONVM_BUCKET/tls/metrics-server-ca.pem` and `s3://YOUR_MONVM_BUCKET/tls/logs-server-ca.pem`. Configure clients to trust the CA for their endpoint and present a client certificate chaining to an enrolled CA. Each TLS identity survives compute replacement and combined/split transitions with its service volume.
