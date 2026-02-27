resource "aws_iam_policy" "iam_policy_cloud_watch" {
  name        = "${var.app}-${var.environment}-iam-policy-cloud-watch"
  description = "Give access to cloud watch to lambdas"

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      }
    ]
  })
}

resource "aws_lambda_function" "lambda_function" {
  function_name    = "${var.app}-${var.environment}-lambda-function"
  role             = aws_iam_role.iam_role_lambda.arn
  runtime          = "provided.al2"
  handler          = "bootstrap"
  filename         = "handler.zip"
  source_code_hash = filebase64sha256("handler.zip")
  timeout          = 20
  memory_size      = 1024
  architectures    = ["arm64"]

  environment {
    variables = {
      ENV                  = var.environment
      STATE_TABLE          = var.state_table_name
      PARAM_PREFIX         = var.param_prefix
      MAX_QUESTION_LENGTH  = tostring(var.max_question_length)
      MAX_CONTEXT_ITEMS    = tostring(var.max_context_items)
      TOKEN_BUDGET         = tostring(var.token_budget)
    }
  }
}

resource "aws_iam_policy" "iam_policy_dynamodb" {
  name        = "${var.app}-${var.environment}-iam-policy-dynamodb"
  description = "Allow Lambda to read/write DynamoDB state table"

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:TransactWriteItems",
          "dynamodb:UpdateItem",
          "dynamodb:Query"
        ]
        Resource = [
          var.state_table_arn,
          "${var.state_table_arn}/index/*"
        ]
      }
    ]
  })
}

resource "aws_iam_policy" "iam_policy_ssm" {
  name        = "${var.app}-${var.environment}-iam-policy-ssm"
  description = "Allow Lambda to read SSM parameters"

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow"
        Action = ["ssm:GetParameter"]
        Resource = [
          "arn:aws:ssm:${var.region}:${var.account_id}:parameter${var.param_prefix}/*"
        ]
      }
    ]
  })
}

resource "aws_iam_role" "iam_role_lambda" {
  name = "${var.app}-${var.environment}-iam-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = "sts:AssumeRole",
        Effect = "Allow",
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_cloudwatch_log_group" "cloudwatch_log_group_lambda_function" {
  name              = "/aws/lambda/${aws_lambda_function.lambda_function.function_name}"
  retention_in_days = 7
}

resource "aws_iam_role_policy_attachment" "iam_role_policy_attachment_event_cloud_watch" {
  role       = aws_iam_role.iam_role_lambda.name
  policy_arn = aws_iam_policy.iam_policy_cloud_watch.arn
}

resource "aws_iam_role_policy_attachment" "iam_role_policy_attachment_dynamodb" {
  role       = aws_iam_role.iam_role_lambda.name
  policy_arn = aws_iam_policy.iam_policy_dynamodb.arn
}

resource "aws_iam_role_policy_attachment" "iam_role_policy_attachment_ssm" {
  role       = aws_iam_role.iam_role_lambda.name
  policy_arn = aws_iam_policy.iam_policy_ssm.arn
}
