# Capacity and Scaling

VictoriaMetrics and VictoriaLogs are designed for efficient single-node operation, but there is no honest universal conversion from instance size to samples or log bytes per second. The result changes with:

- active time-series count and label cardinality;
- scrape interval, churn, and out-of-order data;
- log field structure and repetition, which determine compression;
- concurrent queries, query ranges, and dashboards;
- retention and daily ingest volume;
- EBS throughput and IOPS as well as CPU and RAM.

VictoriaLogs commonly achieves substantial compression on repetitive logs, and its upstream guidance recommends keeping at least 20% disk free and meaningful CPU/RAM headroom. MonVM enforces the 20% disk reserve, but capacity remains workload-specific.

## Practical sizing process

1. Start with the default `t4g.small` combined instance and independent 20 GiB service volumes.
2. Measure CPU, memory, ingest lag, query latency, and on-disk growth during peak load.
3. Keep roughly 50% CPU and RAM headroom for spikes and queries.
4. Increase EBS capacity before retention approaches the 80% service limit. Re-running `setup` with larger volume sizes expands the EBS volumes and filesystems; reductions are rejected.
5. Move to split mode when logs and metrics contend for CPU, memory, or replacement timing.
6. Vertically scale each split VM. MonVM fixes gp3 at the baseline 3,000 IOPS and 125 MiB/s; move beyond its current scope if storage throughput becomes the bottleneck.

The architecture can use very large EC2 instance types, subject to regional availability and each type's ENI limits. That does not imply a guaranteed maximum throughput: the supported ceiling is whatever a representative benchmark sustains with adequate headroom. MonVM intentionally does not add a clustered tier; workloads that outgrow one VM per service should migrate to the upstream clustered products or a managed service.
