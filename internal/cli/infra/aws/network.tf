# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

data "aws_availability_zones" "available" {
  state = "available"
}

data "aws_vpcs" "exact" {
  filter {
    name   = "cidr-block"
    values = [var.vpc_cidr]
  }
}

data "aws_vpc" "existing" {
  count = length(data.aws_vpcs.exact.ids) > 0 ? 1 : 0
  id    = data.aws_vpcs.exact.ids[0]
}

locals {
  matching_vpc_count = length(data.aws_vpcs.exact.ids)
  existing_vpc_owned = local.matching_vpc_count == 1 && try(
    data.aws_vpc.existing[0].tags["monvm:deployment"] == var.name &&
    data.aws_vpc.existing[0].tags["monvm:managed"] == "true",
    false,
  )
  manage_vpc = local.matching_vpc_count == 0 || local.existing_vpc_owned
  reuse_vpc  = local.matching_vpc_count > 0 && !local.existing_vpc_owned
  vpc_id     = local.manage_vpc ? aws_vpc.monvm[0].id : data.aws_vpc.existing[0].id
  existing_ipv6_cidr = local.manage_vpc ? "" : try(one([
    for association in data.aws_vpc.existing[0].ipv6_cidr_block_associations : association.ipv6_cidr_block
    if association.ip_source == "amazon"
  ]), "")
  ipv6_cidr = local.manage_vpc ? aws_vpc.monvm[0].ipv6_cidr_block : local.existing_ipv6_cidr

  availability_zone = var.zone == "" ? sort(data.aws_availability_zones.available.names)[0] : var.zone
  services = {
    metrics = {
      port  = 8428
      cidrs = var.metrics_allowed_cidrs
      ipv4  = var.metrics_ipv4
    }
    logs = {
      port  = 9428
      cidrs = var.logs_allowed_cidrs
      ipv4  = var.logs_ipv4
    }
  }
  ingress = merge([
    for service, definition in local.services : {
      for index, cidr in definition.cidrs : "${service}-${index}" => {
        service = service
        port    = definition.port
        cidr    = cidr
      }
    }
  ]...)
  ingress_ipv4 = { for key, value in local.ingress : key => value if !strcontains(value.cidr, ":") }
  ingress_ipv6 = { for key, value in local.ingress : key => value if strcontains(value.cidr, ":") }
  ipv4_services = {
    for service, definition in local.services : service => definition if definition.ipv4
  }
}

resource "aws_vpc" "monvm" {
  count                            = local.manage_vpc ? 1 : 0
  cidr_block                       = var.vpc_cidr
  assign_generated_ipv6_cidr_block = true
  enable_dns_support               = true
  enable_dns_hostnames             = true
  tags = {
    Name               = var.name
    "monvm:deployment" = var.name
    "monvm:managed"    = "true"
  }
}

moved {
  from = aws_vpc.monvm
  to   = aws_vpc.monvm[0]
}

resource "terraform_data" "vpc_compatibility" {
  input = local.vpc_id
  lifecycle {
    precondition {
      condition     = local.matching_vpc_count <= 1
      error_message = "multiple VPCs have primary CIDR ${var.vpc_cidr}; remove the ambiguity"
    }
    precondition {
      condition     = local.manage_vpc || data.aws_vpc.existing[0].enable_dns_support
      error_message = "existing VPC must enable DNS support"
    }
    precondition {
      condition     = local.manage_vpc || data.aws_vpc.existing[0].enable_dns_hostnames
      error_message = "existing VPC must enable DNS hostnames"
    }
    precondition {
      condition     = try(split("/", local.ipv6_cidr)[1] == "56", false)
      error_message = "existing VPC needs exactly one Amazon-provided IPv6 /56"
    }
    precondition {
      condition     = contains(data.aws_availability_zones.available.names, local.availability_zone)
      error_message = "zone must be an available Availability Zone in the selected region"
    }
    precondition {
      condition     = var.ipv4_network || length(local.ipv4_services) == 0
      error_message = "ipv4_network must be true when an IPv4 service endpoint is enabled"
    }
  }
}

data "aws_subnets" "existing" {
  count = local.reuse_vpc ? 1 : 0
  filter {
    name   = "vpc-id"
    values = [local.vpc_id]
  }
}

data "aws_subnet" "existing" {
  for_each = local.reuse_vpc ? toset(data.aws_subnets.existing[0].ids) : toset([])
  id       = each.value
}

locals {
  vpc_ipv4_prefix   = tonumber(split("/", var.vpc_cidr)[1])
  ipv4_block_prefix = max(24, local.vpc_ipv4_prefix)
  candidate_ipv4_cidrs = flatten([
    for block_index in range(pow(2, local.ipv4_block_prefix - local.vpc_ipv4_prefix)) : [
      for subnet_index in range(pow(2, 28 - local.ipv4_block_prefix)) : cidrsubnet(
        cidrsubnet(var.vpc_cidr, local.ipv4_block_prefix - local.vpc_ipv4_prefix, block_index),
        28 - local.ipv4_block_prefix,
        subnet_index,
      )
    ]
  ])
  owned_ipv4_cidrs = compact([
    for subnet in values(data.aws_subnet.existing) : subnet.cidr_block
    if try(
      subnet.tags["monvm:deployment"] == var.name && subnet.tags["monvm:subnet"] == "true",
      false,
    )
  ])
  occupied_ipv4_cidrs = toset(compact([
    for subnet in values(data.aws_subnet.existing) : subnet.cidr_block
  ]))
  available_ipv4_cidrs = [
    for candidate in local.candidate_ipv4_cidrs : candidate
    if alltrue([
      for occupied in local.occupied_ipv4_cidrs :
      !cidrcontains(occupied, cidrhost(candidate, 0)) && !cidrcontains(candidate, cidrhost(occupied, 0))
    ])
  ]
  subnet_ipv4_cidr = !var.ipv4_network ? "" : (local.manage_vpc ? local.candidate_ipv4_cidrs[0] : (
    length(local.owned_ipv4_cidrs) == 1 ? local.owned_ipv4_cidrs[0] : try(local.available_ipv4_cidrs[0], "")
  ))
  owned_ipv6_cidrs = compact([
    for subnet in values(data.aws_subnet.existing) : subnet.ipv6_cidr_block
    if try(
      subnet.tags["monvm:deployment"] == var.name && subnet.tags["monvm:subnet"] == "true",
      false,
    )
  ])
  occupied_ipv6_cidrs = toset(compact([
    for subnet in values(data.aws_subnet.existing) : subnet.ipv6_cidr_block
  ]))
  available_ipv6_cidrs = [
    for index in range(256) : cidrsubnet(local.ipv6_cidr, 8, index)
    if !contains(local.occupied_ipv6_cidrs, cidrsubnet(local.ipv6_cidr, 8, index))
  ]
  subnet_ipv6_cidr = local.manage_vpc ? cidrsubnet(local.ipv6_cidr, 8, 0) : (
    length(local.owned_ipv6_cidrs) == 1 ? local.owned_ipv6_cidrs[0] : try(local.available_ipv6_cidrs[0], "")
  )
}

data "aws_internet_gateway" "existing" {
  count = local.reuse_vpc ? 1 : 0
  filter {
    name   = "attachment.vpc-id"
    values = [local.vpc_id]
  }
}

resource "aws_internet_gateway" "monvm" {
  count  = local.manage_vpc ? 1 : 0
  vpc_id = local.vpc_id
  tags   = { Name = "${var.name}-internet" }
}

moved {
  from = aws_internet_gateway.monvm
  to   = aws_internet_gateway.monvm[0]
}

resource "aws_subnet" "monvm" {
  vpc_id                                         = local.vpc_id
  availability_zone                              = local.availability_zone
  cidr_block                                     = var.ipv4_network ? local.subnet_ipv4_cidr : null
  ipv6_cidr_block                                = local.subnet_ipv6_cidr
  ipv6_native                                    = !var.ipv4_network
  assign_ipv6_address_on_creation                = true
  map_public_ip_on_launch                        = false
  enable_resource_name_dns_a_record_on_launch    = var.ipv4_network
  enable_resource_name_dns_aaaa_record_on_launch = true
  tags = {
    Name               = var.name
    "monvm:deployment" = var.name
    "monvm:subnet"     = "true"
  }
  depends_on = [terraform_data.vpc_compatibility]
  lifecycle {
    precondition {
      condition     = !var.ipv4_network || local.subnet_ipv4_cidr != ""
      error_message = "existing VPC has no free IPv4 subnet for MonVM"
    }
    precondition {
      condition     = length(local.owned_ipv4_cidrs) <= 1
      error_message = "existing VPC contains multiple IPv4 subnets owned by this MonVM deployment"
    }
    precondition {
      condition     = local.subnet_ipv6_cidr != ""
      error_message = "existing VPC has no free IPv6 /64 for MonVM"
    }
    precondition {
      condition     = length(local.owned_ipv6_cidrs) <= 1
      error_message = "existing VPC contains multiple subnets owned by this MonVM deployment"
    }
  }
}

resource "aws_route_table" "monvm" {
  vpc_id = local.vpc_id
  tags   = { Name = "${var.name}-routes" }
}

resource "aws_route" "internet_ipv6" {
  route_table_id              = aws_route_table.monvm.id
  destination_ipv6_cidr_block = "::/0"
  gateway_id                  = local.manage_vpc ? aws_internet_gateway.monvm[0].id : data.aws_internet_gateway.existing[0].id
}

resource "aws_route" "internet_ipv4" {
  count                  = var.ipv4_network ? 1 : 0
  route_table_id         = aws_route_table.monvm.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = local.manage_vpc ? aws_internet_gateway.monvm[0].id : data.aws_internet_gateway.existing[0].id
}

resource "aws_route_table_association" "monvm" {
  route_table_id = aws_route_table.monvm.id
  subnet_id      = aws_subnet.monvm.id
}

resource "aws_security_group" "management" {
  name_prefix = "${var.name}-management-"
  vpc_id      = local.vpc_id
  egress {
    from_port        = 0
    to_port          = 0
    protocol         = "-1"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }
  tags = { Name = "${var.name}-management" }
}

resource "aws_security_group" "service" {
  for_each    = local.services
  name_prefix = "${var.name}-${each.key}-"
  vpc_id      = local.vpc_id
  egress {
    from_port        = 0
    to_port          = 0
    protocol         = "-1"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }
  tags = { Name = "${var.name}-${each.key}" }
}

resource "aws_vpc_security_group_ingress_rule" "service_ipv6" {
  for_each          = local.ingress_ipv6
  security_group_id = aws_security_group.service[each.value.service].id
  cidr_ipv6         = each.value.cidr
  ip_protocol       = "tcp"
  from_port         = each.value.port
  to_port           = each.value.port
  description       = "MonVM ${each.value.service} endpoint"
}

moved {
  from = aws_vpc_security_group_ingress_rule.service
  to   = aws_vpc_security_group_ingress_rule.service_ipv6
}

resource "aws_vpc_security_group_ingress_rule" "service_ipv4" {
  for_each          = local.ingress_ipv4
  security_group_id = aws_security_group.service[each.value.service].id
  cidr_ipv4         = each.value.cidr
  ip_protocol       = "tcp"
  from_port         = each.value.port
  to_port           = each.value.port
  description       = "MonVM ${each.value.service} endpoint"
}

resource "aws_network_interface" "service" {
  for_each           = local.services
  subnet_id          = aws_subnet.monvm.id
  ipv6_address_count = 1
  security_groups    = [aws_security_group.service[each.key].id]
  tags               = { Name = "${var.name}-${each.key}" }
}

resource "aws_eip" "service" {
  for_each                  = local.ipv4_services
  domain                    = "vpc"
  network_interface         = aws_network_interface.service[each.key].id
  associate_with_private_ip = aws_network_interface.service[each.key].private_ip
  tags                      = { Name = "${var.name}-${each.key}" }
  depends_on                = [aws_route.internet_ipv4]
}

output "metrics_endpoint" {
  description = "Stable mTLS VictoriaMetrics endpoint."
  value       = "https://[${one(aws_network_interface.service["metrics"].ipv6_addresses)}]:8428"
}

output "logs_endpoint" {
  description = "Stable mTLS VictoriaLogs endpoint."
  value       = "https://[${one(aws_network_interface.service["logs"].ipv6_addresses)}]:9428"
}

output "metrics_ipv4_endpoint" {
  description = "Optional stable public IPv4 mTLS VictoriaMetrics endpoint."
  value       = var.metrics_ipv4 ? "https://${aws_eip.service["metrics"].public_ip}:8428" : null
}

output "logs_ipv4_endpoint" {
  description = "Optional stable public IPv4 mTLS VictoriaLogs endpoint."
  value       = var.logs_ipv4 ? "https://${aws_eip.service["logs"].public_ip}:9428" : null
}
