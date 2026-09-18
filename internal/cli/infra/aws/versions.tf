# MonVM <https://monvm.dev>
# Copyright The MonVM Authors
# SPDX-License-Identifier: Apache-2.0

terraform {
  required_version = ">= 1.11"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
  backend "s3" {}
}

provider "aws" {
  region  = var.region
  profile = var.profile == "" ? null : var.profile

  default_tags {
    tags = {
      "monvm:deployment" = var.name
      "monvm:managed"    = "true"
    }
  }
}
