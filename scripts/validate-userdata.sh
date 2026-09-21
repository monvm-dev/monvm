#!/usr/bin/env bash
# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
temporary=$(mktemp -d)
trap 'rm -rf "$temporary"' EXIT
grep -Fq 'user_data_base64 = base64gzip(templatefile(' "$root/internal/cli/infra/aws/compute.tf"
cp "$root/internal/cli/infra/aws/userdata.sh.tftpl" "$temporary/userdata.sh.tftpl"
cat >"$temporary/expression" <<'EOF'
templatefile("userdata.sh.tftpl", {
  region = "us-east-1", bucket = "monvm-test", architecture = "arm64",
  metrics_enabled = true, logs_enabled = true,
  metrics_ipv6 = "2001:db8::1", logs_ipv6 = "2001:db8::2",
  metrics_ipv4 = "198.51.100.1", logs_ipv4 = "198.51.100.2",
  metrics_private_ipv4 = "10.73.0.4", logs_private_ipv4 = "10.73.0.5",
  metrics_mac = "02:00:00:00:00:01", logs_mac = "02:00:00:00:00:02",
  subnet_ipv4_cidr = "10.73.0.0/24", subnet_ipv4_gateway = "10.73.0.1", subnet_ipv6_cidr = "2001:db8::/64",
  metrics_volume_id = "vol-0123456789abcdef0", logs_volume_id = "vol-0123456789abcdef1",
  metrics_volume_size = 20, logs_volume_size = 20,
  metrics_object_key = "dependencies/metrics.tar.gz", metrics_sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  metrics_binary = "victoria-metrics-prod", metrics_retention = "90d",
  logs_object_key = "dependencies/logs.tar.gz", logs_sha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  logs_binary = "victoria-logs-prod", logs_retention = "14d",
  envoy_object_key = "dependencies/envoy", envoy_sha256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
  trust_enroll = filebase64("${root}/internal/cli/infra/aws/trust-enroll.sh"),
  trust_refresh = filebase64("${root}/internal/cli/infra/aws/trust-refresh.sh")
})
EOF
(sed -i.bak "s|\${root}|$root|g" "$temporary/expression" && rm "$temporary/expression.bak")

for combination in both metrics logs; do
  case "$combination" in
    both) metrics_enabled=true; logs_enabled=true ;;
    metrics) metrics_enabled=true; logs_enabled=false ;;
    logs) metrics_enabled=false; logs_enabled=true ;;
  esac
  sed -e "s/metrics_enabled = true/metrics_enabled = $metrics_enabled/" \
    -e "s/logs_enabled = true/logs_enabled = $logs_enabled/" \
    "$temporary/expression" >"$temporary/expression-$combination"
  (cd "$temporary" && tofu console <"expression-$combination") |
    sed '1d; $d; s/\\"/"/g; s/\\\\/\\/g' >"$temporary/userdata-$combination.sh"
  bash -n "$temporary/userdata-$combination.sh"
  shellcheck "$temporary/userdata-$combination.sh"
  if [[ "$metrics_enabled" == true ]]; then
    grep -Fq 'mount_volume "vol-0123456789abcdef0" /var/lib/victoria-metrics "20"' "$temporary/userdata-$combination.sh"
    grep -Fq "'/usr/local/bin/victoria-metrics -httpListenAddr=127.0.0.1:18428" "$temporary/userdata-$combination.sh"
    grep -Fq 'configure_service metrics VictoriaMetrics "2001:db8::1" "198.51.100.1" "10.73.0.4" "02:00:00:00:00:01" 100' "$temporary/userdata-$combination.sh"
    grep -Fq 'write_envoy_listener metrics 8428 /var/lib/victoria-metrics' "$temporary/userdata-$combination.sh"
    grep -Fq 'write_envoy_cluster metrics 18428' "$temporary/userdata-$combination.sh"
  elif grep -Fq 'configure_service metrics VictoriaMetrics' "$temporary/userdata-$combination.sh"; then
    echo "metrics service rendered for $combination" >&2
    exit 1
  fi
  if [[ "$logs_enabled" == true ]]; then
    grep -Fq 'mount_volume "vol-0123456789abcdef1" /var/lib/victoria-logs "20"' "$temporary/userdata-$combination.sh"
    grep -Fq "'/usr/local/bin/victoria-logs -httpListenAddr=127.0.0.1:19428" "$temporary/userdata-$combination.sh"
    grep -Fq 'configure_service logs VictoriaLogs "2001:db8::2" "198.51.100.2" "10.73.0.5" "02:00:00:00:00:02" 101' "$temporary/userdata-$combination.sh"
    grep -Fq 'write_envoy_listener logs 9428 /var/lib/victoria-logs' "$temporary/userdata-$combination.sh"
    grep -Fq 'write_envoy_cluster logs 19428' "$temporary/userdata-$combination.sh"
  elif grep -Fq 'configure_service logs VictoriaLogs' "$temporary/userdata-$combination.sh"; then
    echo "logs service rendered for $combination" >&2
    exit 1
  fi
  [[ $(grep -c '^systemctl start envoy$' "$temporary/userdata-$combination.sh") -eq 1 ]]
  if grep -Eq 'envoy-(metrics|logs)' "$temporary/userdata-$combination.sh"; then
    echo "multiple Envoy services rendered for $combination" >&2
    exit 1
  fi
  {
    printf 'base64gzip('
    cat "$temporary/expression-$combination"
    printf ')\n'
  } >"$temporary/expression-$combination-gzip"
  (cd "$temporary" && tofu console <"expression-$combination-gzip") |
    tr -d '"\n' | openssl base64 -d -A >"$temporary/userdata-$combination.sh.gz"
  compressed_size=$(wc -c <"$temporary/userdata-$combination.sh.gz")
  if ((compressed_size > 16384)); then
    echo "$combination user data compresses to $compressed_size bytes; EC2 allows at most 16384" >&2
    exit 1
  fi
  gzip -t "$temporary/userdata-$combination.sh.gz"
done

sed -e 's/metrics_ipv4 = "198.51.100.1"/metrics_ipv4 = ""/' \
  -e 's/logs_ipv4 = "198.51.100.2"/logs_ipv4 = ""/' \
  -e 's/metrics_private_ipv4 = "10.73.0.4"/metrics_private_ipv4 = ""/' \
  -e 's/logs_private_ipv4 = "10.73.0.5"/logs_private_ipv4 = ""/' \
  -e 's/subnet_ipv4_cidr = "10.73.0.0\/24"/subnet_ipv4_cidr = ""/' \
  -e 's/subnet_ipv4_gateway = "10.73.0.1"/subnet_ipv4_gateway = ""/' \
  "$temporary/expression" >"$temporary/expression-ipv6-only"
(cd "$temporary" && tofu console <expression-ipv6-only) |
  sed '1d; $d; s/\\"/"/g; s/\\\\/\\/g' >"$temporary/userdata-ipv6-only.sh"
bash -n "$temporary/userdata-ipv6-only.sh"
shellcheck "$temporary/userdata-ipv6-only.sh"
grep -Fq 'configure_service metrics VictoriaMetrics "2001:db8::1" "" "" "02:00:00:00:00:01" 100' "$temporary/userdata-ipv6-only.sh"
grep -Fq 'configure_service logs VictoriaLogs "2001:db8::2" "" "" "02:00:00:00:00:02" 101' "$temporary/userdata-ipv6-only.sh"
grep -Fq "monvm-configure-service-network \$mac \${private_ipv4:--} - - \$ipv6 2001:db8::/64 \$table" "$temporary/userdata-ipv6-only.sh"

grep -Fq "resize2fs \"\$device\"" "$temporary/userdata-both.sh"
grep -Fq -- '--endpoint-url "https://s3.dualstack.us-east-1.amazonaws.com" --only-show-errors' "$temporary/userdata-both.sh"
grep -Fq "subjectAltName=\$san" "$temporary/userdata-both.sh"
grep -Fq "ExecStart=\$command" "$temporary/userdata-both.sh"
grep -Fq "ip rule add priority \"\$table\" from \"\$ipv4/32\" table \"\$table\"" "$temporary/userdata-both.sh"
grep -Fq "aws s3 cp \"\$root/envoy/server-ca.crt\"" "$temporary/userdata-both.sh"
grep -Fq '/usr/local/bin/envoy -c /etc/envoy/envoy.yaml --log-level info' "$temporary/userdata-both.sh"
grep -Fq 'port_value: 9900' "$temporary/userdata-both.sh"
ssm_line=$(grep -n '^systemctl restart amazon-ssm-agent$' "$temporary/userdata-both.sh" | cut -d: -f1)
apt_line=$(grep -n '^apt-get update$' "$temporary/userdata-both.sh" | cut -d: -f1)
[[ "$ssm_line" -lt "$apt_line" ]]

openssl req -x509 -newkey ed25519 -nodes -days 1 -subj /CN=MonVM-test-CA \
  -addext basicConstraints=critical,CA:TRUE -addext keyUsage=critical,keyCertSign,cRLSign \
  -keyout "$temporary/ca.key" -out "$temporary/ca.crt" >/dev/null 2>&1
openssl req -new -newkey ed25519 -nodes -subj /CN=MonVM-test \
  -addext basicConstraints=critical,CA:FALSE -addext keyUsage=critical,digitalSignature \
  -addext extendedKeyUsage=serverAuth -addext subjectAltName=IP:2001:db8::1,IP:198.51.100.1 \
  -keyout "$temporary/server.key" -out "$temporary/server.csr" >/dev/null 2>&1
openssl x509 -req -days 1 -in "$temporary/server.csr" -CA "$temporary/ca.crt" \
  -CAkey "$temporary/ca.key" -CAcreateserial -copy_extensions copy \
  -out "$temporary/server.crt" >/dev/null 2>&1
openssl verify -CAfile "$temporary/ca.crt" "$temporary/server.crt" >/dev/null
openssl x509 -in "$temporary/server.crt" -noout -checkip 2001:db8::1 >/dev/null
openssl x509 -in "$temporary/server.crt" -noout -checkip 198.51.100.1 >/dev/null

shellcheck "$root/internal/cli/infra/aws/trust-enroll.sh" \
  "$root/internal/cli/infra/aws/trust-refresh.sh" "$0"
