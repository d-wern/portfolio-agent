provider "aws" {
  region = var.region
}

terraform {
  backend "s3" {
  }
}

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.21.0"
    }
  }
}

module "dynamodb" {
  source = "./modules/dynamodb"
  app    = var.app
}

module "api-gateway" {
  source      = "./modules/api-gateway"
  app         = var.app
  account_id  = var.account_id
  region      = var.region
  environment = var.environment
}

module "lambda" {
  source           = "./modules/lambda"
  app              = var.app
  environment      = var.environment
  state_table_name = module.dynamodb.table_name
  state_table_arn  = module.dynamodb.table_arn
  param_prefix     = "/${var.app}"
  account_id       = var.account_id
  region           = var.region
  token_budget     = var.token_budget
}

module "ssm_parameters" {
  source = "./modules/ssm-parameters"
  app    = var.app
}
