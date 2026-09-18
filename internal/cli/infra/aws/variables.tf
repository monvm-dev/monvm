# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

variable "name" {
  type        = string
  description = "MonVM deployment identifier."
  validation {
    condition     = can(regex("^[a-z]([a-z0-9-]{0,30}[a-z0-9])?$", var.name))
    error_message = "name must be a valid MonVM deployment identifier."
  }
}
variable "region" {
  type        = string
  description = "AWS region containing the deployment."
}
variable "profile" {
  type        = string
  description = "Optional AWS shared configuration profile."
  default     = ""
}
variable "bucket" {
  type        = string
  description = "Private deployment bucket used for state and artifacts."
}
variable "vpc_cidr" {
  type        = string
  description = "Primary private IPv4 CIDR used to find or create the VPC."
  validation {
    condition = can(cidrhost(var.vpc_cidr, 0)) && !can(regex(":", var.vpc_cidr)) && try(
      tonumber(split("/", var.vpc_cidr)[1]) >= 16 && tonumber(split("/", var.vpc_cidr)[1]) <= 28,
      false,
    )
    error_message = "vpc_cidr must be an IPv4 CIDR between /16 and /28."
  }
}
variable "zone" {
  type        = string
  description = "Availability Zone for compute and persistent volumes."
  default     = ""
}
variable "mode" {
  type        = string
  description = "Compute layout: combined or split."
  validation {
    condition     = contains(["combined", "split"], var.mode)
    error_message = "mode must be combined or split."
  }
}
variable "active" {
  type        = bool
  description = "Whether EC2 compute and service attachments are present."
}
variable "architecture" {
  type        = string
  description = "CPU architecture for EC2 compute and release artifacts."
  validation {
    condition     = contains(["amd64", "arm64"], var.architecture)
    error_message = "architecture must be amd64 or arm64."
  }
}
variable "combined_instance_type" {
  type        = string
  description = "EC2 instance type used in combined mode."
}
variable "metrics_instance_type" {
  type        = string
  description = "EC2 instance type used for metrics in split mode."
}
variable "logs_instance_type" {
  type        = string
  description = "EC2 instance type used for logs in split mode."
}
variable "metrics_volume_size" {
  type        = number
  description = "Metrics gp3 volume size in GiB."
  validation {
    condition     = var.metrics_volume_size >= 1 && floor(var.metrics_volume_size) == var.metrics_volume_size
    error_message = "metrics_volume_size must be a positive whole number."
  }
}
variable "logs_volume_size" {
  type        = number
  description = "Logs gp3 volume size in GiB."
  validation {
    condition     = var.logs_volume_size >= 1 && floor(var.logs_volume_size) == var.logs_volume_size
    error_message = "logs_volume_size must be a positive whole number."
  }
}
variable "metrics_retention" {
  type        = string
  description = "VictoriaMetrics retention period."
  validation {
    condition     = can(regex("^[1-9][0-9]*(h|d|w|y)$", var.metrics_retention))
    error_message = "metrics_retention must be a positive number followed by h, d, w, or y."
  }
}
variable "logs_retention" {
  type        = string
  description = "VictoriaLogs retention period."
  validation {
    condition     = can(regex("^[1-9][0-9]*(h|d|w|y)$", var.logs_retention))
    error_message = "logs_retention must be a positive number followed by h, d, w, or y."
  }
}
variable "ipv4_network" {
  type        = bool
  description = "Whether the persistent service subnet has been converted to dual-stack."
  default     = false
}
variable "metrics_ipv4" {
  type        = bool
  description = "Whether to allocate a stable public IPv4 address for metrics."
  default     = false
}
variable "logs_ipv4" {
  type        = bool
  description = "Whether to allocate a stable public IPv4 address for logs."
  default     = false
}
variable "metrics_allowed_cidrs" {
  type        = list(string)
  description = "IPv4 and IPv6 CIDRs allowed to reach the metrics endpoint."
  default     = []
}
variable "logs_allowed_cidrs" {
  type        = list(string)
  description = "IPv4 and IPv6 CIDRs allowed to reach the logs endpoint."
  default     = []
}
variable "metrics_artifact" {
  description = "Verified VictoriaMetrics artifact mirrored into S3."
  type = object({
    object_key = string
    sha256     = string
    binary     = string
  })
}
variable "logs_artifact" {
  description = "Verified VictoriaLogs artifact mirrored into S3."
  type = object({
    object_key = string
    sha256     = string
    binary     = string
  })
}
variable "envoy_artifact" {
  description = "Verified Envoy artifact mirrored into S3."
  type = object({
    object_key = string
    sha256     = string
    binary     = string
  })
}
