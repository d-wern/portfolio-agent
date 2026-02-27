variable "account_id" {
  type    = string
  default = "600627353779"
}

variable "region" {
  type    = string
  default = "eu-north-1"
}

variable "environment" {
  type    = string
  default = "dev"
}

variable "app" {
  type    = string
  default = "portfolio-agent"
}

variable "token_budget" {
  type    = number
  default = 6000
}
