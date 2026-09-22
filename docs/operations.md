# Operations and Security

## Access

Instances have no SSH ingress. Use AWS Systems Manager Session Manager for diagnostics. Service security groups allow only explicitly configured IPv4 and IPv6 CIDRs. VictoriaMetrics and VictoriaLogs listen only on loopback; one Envoy process per VM listens on its service ENIs.

Run `monvm port-forward NAME` for temporary localhost access to both UIs without opening ingress. The command starts one Systems Manager port-forwarding session per service, including when both services share a combined instance, and stops both sessions on Ctrl-C.

Both external service endpoints use Envoy, TLS 1.3, and mandatory client certificates in addition to CIDR filtering. If no valid client CA objects are enrolled, that listener uses an unreachable throwaway authority and denies every client.

Envoy reads each service's combined trust bundle through filesystem SDS. Existing source IDs refresh from S3 every 10 minutes and valid changes are installed atomically without restarting Envoy or dropping established connections. Deleting an enrolled object revokes that source on the next successful check. Invalid content and transient S3 failures retain the last-known-good certificate, preventing a broken publisher update from interrupting ingestion.

## Updates and replacement

Re-run `setup` to apply configuration changes or pick up current upstream releases. Every archive is selected by exact OSS filename and verified against GitHub's release digest before upload and again during VM boot. User-data changes replace compute; data volumes and service ENIs remain.

Combined/split transitions and instance replacements are not highly available. Plan for downtime while EBS volumes and ENIs detach and reattach. Most log forwaders will easily tolerate a few minutes of downtime.

## Deployment state and artifacts

The private S3 bucket enforces bucket-owner ownership, blocks public access, and stores release mirrors, deployment metadata, TF state, lockfiles, client CA store objects, and the public service certificates. Object versioning is disabled; `setup` suspends it on a bucket where it was previously enabled. Treat TF state as sensitive and restrict operator access. Give each external client CA publisher `s3:PutObject` access only to its exact `trust/SOURCE_ID.pem` key; reserve deletion and broader access for the MonVM operator.

## Backup and recovery

Encrypted EBS is durable block storage, not a backup. Configure EBS snapshots or AWS Backup separately if the data must survive accidental deletion or corruption. `destroy` intentionally deletes the volumes and stable addresses. It does not preserve a snapshot.

## Cost controls

`teardown` removes EC2 cost but continues charging for EBS volumes, enabled EIPs, ENIs/public IPv6 where applicable, S3, and retained network resources. AWS charges for public IPv4 addresses whether attached to running compute or retained while compute is absent. Disable an unneeded IPv4 endpoint to release its EIP, or use `destroy` to remove the entire deployment. Review current AWS prices in your region; MonVM does not estimate prices because they change independently of the CLI.
