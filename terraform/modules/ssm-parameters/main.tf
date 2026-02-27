resource "aws_ssm_parameter" "resume" {
  name      = "/${var.app}/resume"
  type      = "String"
  value     = "{}"
  overwrite = false

  lifecycle {
    ignore_changes = [value]
  }
}

resource "aws_ssm_parameter" "interests" {
  name      = "/${var.app}/interests"
  type      = "String"
  value     = "[]"
  overwrite = false

  lifecycle {
    ignore_changes = [value]
  }
}

resource "aws_ssm_parameter" "pinned_prompt" {
  name      = "/${var.app}/pinned_prompt"
  type      = "String"
  value     = "You are a helpful assistant representing the portfolio owner. Answer questions based only on the provided resume, interests, and conversation history. Speak in the first person with a professional, concise tone. If the information is not available, say: \"I don't have that information.\""
  overwrite = false

  lifecycle {
    ignore_changes = [value]
  }
}

resource "aws_ssm_parameter" "openai_model" {
  name      = "/${var.app}/config/openai_model"
  type      = "String"
  value     = "gpt-4o-mini"
  overwrite = false

  lifecycle {
    ignore_changes = [value]
  }
}

resource "aws_ssm_parameter" "open_ai_token" {
  name      = "/${var.app}/open-ai-token"
  type      = "SecureString"
  value     = "{}"
  overwrite = false

  lifecycle {
    ignore_changes = [value]
  }
}
