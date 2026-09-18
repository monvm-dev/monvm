# Architecture and Infrastructure

Each deployment owns:

- One IPv6-native subnet and route table, converted permanently to dual-stack when an IPv4 endpoint is first enabled.
- One VPC and internet gateway only when no existing VPC has the requested primary IPv4 CIDR.
- Two persistent service ENIs: one for metrics and one for logs, with an optional EIP on either after dual-stack conversion.
- Two encrypted gp3 EBS volumes: one for metrics data and one for logs data.
- One private S3 bucket containing TF state, release mirrors, deployment metadata, client CA store objects, and both service certificates.
- One IAM role and instance profile with SSM access plus narrowly scoped dependency, client CA store, and server-certificate object access.
- One combined EC2 instance or two split EC2 instances.

The EC2 root volume is disposable. Service data lives only on the independently managed EBS volumes. The service ENIs keep their public IPv6 addresses and enabled EIPs when compute is replaced. A combined instance has a management interface plus both service interfaces; a split instance has a management interface plus its service interface.

VPC selection is based on exact primary-CIDR matches. Zero matches creates a tagged, MonVM-owned VPC. One compatible match is reused without taking ownership; multiple matches fail. Reuse requires DNS support and hostnames, exactly one Amazon-provided IPv6 `/56`, exactly one attached internet gateway, and a free IPv6 `/64`. When IPv4 is enabled, MonVM also selects a free IPv4 `/28`. `destroy` removes MonVM resources from a reused VPC but leaves the VPC, its internet gateway, and unrelated resources intact.

## Boot process

Debian user data downloads and starts the SSM Agent before running `apt-get`, so Session Manager diagnostics become available before the slower package installation. It then installs the AWS CLI, filesystem tooling, OpenSSL, Python, and NVMe tooling. Python parses the bounded S3 trust listing, while NVMe tooling maps persistent EBS volume IDs to their actual Nitro device names. The boot process creates ext4 only when a volume has no filesystem, mounts by filesystem UUID, expands the filesystem to the current EBS size, downloads mirrored dependencies from the deployment's S3 bucket over a dual-stack endpoint, verifies SHA-256 again, and installs systemd services under unprivileged users.

VictoriaMetrics uses `/var/lib/victoria-metrics`; VictoriaLogs uses `/var/lib/victoria-logs`. Both native services bind only to loopback. Each VM runs one Envoy process: combined mode configures metrics and logs listeners in that process, while each split VM configures only its assigned listener. Boot-time systemd units configure the persistent service ENIs and source-specific IPv4 and IPv6 routing before Envoy starts. VictoriaLogs stops ingesting before exhausting the disk through `-retention.maxDiskUsagePercent=80`.

Each service volume also holds its stable private server CA, replaceable endpoint certificate, and last-known-good client CA bundles. Endpoint certificates can gain or lose address SANs without changing the CA trusted by clients. At boot, `monvm-trust-enroll` lists up to 1,000 objects under `trust/`, records only keys shaped like `trust/SOURCE_ID.pem`, removes sources no longer listed, and performs the initial refresh. Every 10 minutes, `monvm-trust-refresh` re-fetches only those enrolled keys, validates that each PEM contains CA certificates and no private keys, retains last-known-good files on transient failures, and atomically replaces the service's combined bundle watched by Envoy through filesystem SDS. New keys are deliberately not discovered until the next boot.

## Generated infrastructure

The rendered TF files and variables are retained in the MonVM cache directory (`$XDG_CACHE_HOME/monvm` when set). TF state uses the deployment bucket's S3 backend and native S3 lockfiles.

`teardown` sets the module's `active` variable to false. Compute and attachments are removed; ENIs, addresses, EBS volumes, networking, S3, and state remain. `setup` sets it true again. `destroy` destroys every MonVM-owned resource and then empties and deletes the bucket; a reused VPC and internet gateway remain.
