resource "aws_api_gateway_rest_api" "api_gateway_rest_api" {
  name = "${var.app}-${var.environment}-api-gateway-rest-api"
  body = templatefile("${path.module}/open-api.yml", {
    region     = var.region
    account_id = var.account_id
    env        = var.environment
    app        = var.app
  })
}

resource "aws_api_gateway_stage" "api_gateway_stage" {
  deployment_id = aws_api_gateway_deployment.api_gateway_deployment.id
  rest_api_id   = aws_api_gateway_rest_api.api_gateway_rest_api.id
  stage_name    = var.environment

  depends_on = [
    aws_api_gateway_account.api_gateway_account,
    aws_iam_role_policy_attachment.api_gateway_cloudwatch_role_attachment,
  ]
}

resource "aws_api_gateway_deployment" "api_gateway_deployment" {
  rest_api_id = aws_api_gateway_rest_api.api_gateway_rest_api.id

  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_rest_api.api_gateway_rest_api,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_method_settings" "api_gateway_method_settings" {
  rest_api_id = aws_api_gateway_rest_api.api_gateway_rest_api.id
  stage_name  = aws_api_gateway_stage.api_gateway_stage.stage_name
  method_path = "*/*"
  settings {
    logging_level      = "INFO"
    data_trace_enabled = false
    metrics_enabled    = true
  }

  depends_on = [
    aws_api_gateway_account.api_gateway_account,
    aws_iam_role.api_gateway_cloudwatch_role,
    aws_iam_role_policy_attachment.api_gateway_cloudwatch_role_attachment,
  ]
}

resource "aws_lambda_permission" "lambda_permission_api_gateway" {
  statement_id  = "AllowAPIGatewayInvokeEvent"
  action        = "lambda:InvokeFunction"
  function_name = "${var.app}-${var.environment}-lambda-function"
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.api_gateway_rest_api.execution_arn}/*/*"
}

resource "aws_iam_role" "api_gateway_cloudwatch_role" {
  name = "${var.app}-${var.environment}-apigateway-cloudwatch-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow",
        Principal = {
          Service = "apigateway.amazonaws.com"
        },
        Action = "sts:AssumeRole"
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "api_gateway_cloudwatch_role_attachment" {
  role       = aws_iam_role.api_gateway_cloudwatch_role.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonAPIGatewayPushToCloudWatchLogs"
}

resource "aws_api_gateway_account" "api_gateway_account" {
  cloudwatch_role_arn = aws_iam_role.api_gateway_cloudwatch_role.arn
  depends_on = [
    aws_iam_role_policy_attachment.api_gateway_cloudwatch_role_attachment,
  ]
}
