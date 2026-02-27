variable "environment" {
  type = string
}

variable "app" {
  type = string
}

variable "state_table_name" {
  type        = string
  description = "Name of the DynamoDB state table"
}

variable "state_table_arn" {
  type        = string
  description = "ARN of the DynamoDB state table"
}

variable "param_prefix" {
  type        = string
  description = "SSM parameter prefix (e.g. /portfolio-agent)"
}

variable "account_id" {
  type        = string
  description = "AWS account ID, used to build SSM parameter ARNs"
}

variable "region" {
  type        = string
  description = "AWS region, used to build SSM parameter ARNs"
}

variable "max_question_length" {
  type        = number
  default     = 300
  description = "Maximum allowed question length in characters"
}

variable "max_context_items" {
  type        = number
  default     = 20
  description = "Maximum number of conversation history items to load"
}

variable "token_budget" {
  type        = number
  default     = 6000
  description = "Maximum prompt token budget before calling OpenAI"
}
