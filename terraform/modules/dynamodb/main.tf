resource "aws_dynamodb_table" "agent_questions" {
  name         = "agent-questions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "PK"
  range_key    = "SK"

  attribute {
    name = "PK"
    type = "S"
  }

  attribute {
    name = "SK"
    type = "S"
  }

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  tags = {
    App = var.app
  }
}

output "table_name" {
  value = aws_dynamodb_table.agent_questions.name
}

output "table_arn" {
  value = aws_dynamodb_table.agent_questions.arn
}

