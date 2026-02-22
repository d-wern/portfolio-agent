package main

import (
	"github.com/aws/aws-lambda-go/lambda"

	"portfolio-agent/handler"
)

func main() {
	lambda.Start(handler.Handler)
}
