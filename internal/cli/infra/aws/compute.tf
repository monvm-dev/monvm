# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

data "aws_ami" "debian" {
  most_recent = true
  owners      = ["136693071363"]
  filter {
    name   = "name"
    values = ["debian-13-${var.architecture}-*"]
  }
  filter {
    name   = "state"
    values = ["available"]
  }
}

locals {
  all_hosts = var.mode == "combined" ? {
    combined = {
      instance_type  = var.combined_instance_type
      metrics        = true
      logs           = true
      metrics_device = 1
      logs_device    = 2
    }
    } : {
    metrics = {
      instance_type  = var.metrics_instance_type
      metrics        = true
      logs           = false
      metrics_device = 1
      logs_device    = 0
    }
    logs = {
      instance_type  = var.logs_instance_type
      metrics        = false
      logs           = true
      metrics_device = 0
      logs_device    = 1
    }
  }
  hosts = var.active ? local.all_hosts : {}
  service_attachments = var.mode == "combined" ? {
    metrics = { host = "combined", device = 1 }
    logs    = { host = "combined", device = 2 }
    } : {
    metrics = { host = "metrics", device = 1 }
    logs    = { host = "logs", device = 1 }
  }
  active_service_attachments = var.active ? local.service_attachments : {}
}

resource "aws_ebs_volume" "service" {
  for_each = {
    metrics = var.metrics_volume_size
    logs    = var.logs_volume_size
  }
  availability_zone = local.availability_zone
  encrypted         = true
  type              = "gp3"
  size              = each.value
  iops              = 3000
  throughput        = 125
  tags              = { Name = "${var.name}-${each.key}" }
}

resource "aws_iam_role" "instance" {
  name_prefix = "${var.name}-"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy" "dependencies" {
  role = aws_iam_role.instance.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = ["arn:aws:s3:::${var.bucket}/dependencies/*", "arn:aws:s3:::${var.bucket}/trust/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["s3:PutObject"]
        Resource = ["arn:aws:s3:::${var.bucket}/tls/metrics-server-ca.pem", "arn:aws:s3:::${var.bucket}/tls/logs-server-ca.pem"]
      },
      {
        Effect   = "Allow"
        Action   = ["s3:ListBucket"]
        Resource = "arn:aws:s3:::${var.bucket}"
        Condition = {
          StringEquals = { "s3:prefix" = ["trust/"] }
        }
      }
    ]
  })
}

resource "aws_iam_instance_profile" "instance" {
  name_prefix = "${var.name}-"
  role        = aws_iam_role.instance.name
}

resource "aws_instance" "host" {
  for_each      = local.hosts
  ami           = data.aws_ami.debian.id
  instance_type = each.value.instance_type
  subnet_id     = aws_subnet.monvm.id
  vpc_security_group_ids = [
    aws_security_group.management.id,
  ]
  ipv6_address_count          = 1
  iam_instance_profile        = aws_iam_instance_profile.instance.name
  user_data_replace_on_change = true
  user_data_base64 = base64gzip(templatefile("${path.module}/userdata.sh.tftpl", {
    region               = var.region
    bucket               = var.bucket
    architecture         = var.architecture
    metrics_enabled      = each.value.metrics
    logs_enabled         = each.value.logs
    metrics_ipv6         = one(aws_network_interface.service["metrics"].ipv6_addresses)
    logs_ipv6            = one(aws_network_interface.service["logs"].ipv6_addresses)
    metrics_ipv4         = try(aws_eip.service["metrics"].public_ip, "")
    logs_ipv4            = try(aws_eip.service["logs"].public_ip, "")
    metrics_private_ipv4 = var.ipv4_network ? aws_network_interface.service["metrics"].private_ip : ""
    logs_private_ipv4    = var.ipv4_network ? aws_network_interface.service["logs"].private_ip : ""
    metrics_mac          = aws_network_interface.service["metrics"].mac_address
    logs_mac             = aws_network_interface.service["logs"].mac_address
    subnet_ipv4_cidr     = var.ipv4_network ? aws_subnet.monvm.cidr_block : ""
    subnet_ipv4_gateway  = var.ipv4_network ? cidrhost(aws_subnet.monvm.cidr_block, 1) : ""
    subnet_ipv6_cidr     = aws_subnet.monvm.ipv6_cidr_block
    metrics_volume_id    = aws_ebs_volume.service["metrics"].id
    logs_volume_id       = aws_ebs_volume.service["logs"].id
    metrics_volume_size  = var.metrics_volume_size
    logs_volume_size     = var.logs_volume_size
    metrics_object_key   = var.metrics_artifact.object_key
    metrics_sha256       = var.metrics_artifact.sha256
    metrics_binary       = var.metrics_artifact.binary
    metrics_retention    = var.metrics_retention
    logs_object_key      = var.logs_artifact.object_key
    logs_sha256          = var.logs_artifact.sha256
    logs_binary          = var.logs_artifact.binary
    logs_retention       = var.logs_retention
    envoy_object_key     = var.envoy_artifact.object_key
    envoy_sha256         = var.envoy_artifact.sha256
    trust_enroll         = filebase64("${path.module}/trust-enroll.sh")
    trust_refresh        = filebase64("${path.module}/trust-refresh.sh")
  }))
  root_block_device {
    encrypted   = true
    volume_type = "gp3"
    volume_size = 12
  }
  metadata_options {
    http_endpoint               = "enabled"
    http_protocol_ipv6          = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }
  depends_on = [
    aws_iam_role_policy.dependencies,
    aws_iam_role_policy_attachment.ssm,
  ]
  tags = { Name = "${var.name}-${each.key}" }
}

resource "aws_network_interface_attachment" "service" {
  for_each             = local.active_service_attachments
  instance_id          = aws_instance.host[each.value.host].id
  network_interface_id = aws_network_interface.service[each.key].id
  device_index         = each.value.device
}

resource "aws_volume_attachment" "service" {
  for_each    = local.active_service_attachments
  instance_id = aws_instance.host[each.value.host].id
  volume_id   = aws_ebs_volume.service[each.key].id
  device_name = each.key == "metrics" ? "/dev/sdf" : "/dev/sdg"
}
