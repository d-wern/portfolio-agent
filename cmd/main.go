package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	awsdynamodb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awsssm "github.com/aws/aws-sdk-go-v2/service/ssm"

	"portfolio-agent/handler"
	"portfolio-agent/internal/integrations/openai"
	"portfolio-agent/internal/integrations/paramstore"
	"portfolio-agent/internal/repository"
	"portfolio-agent/internal/usecase"
)

func main() {
	ctx := context.Background()

	// ---- Configuration (read only here) ----
	stateTable := mustEnv("STATE_TABLE")
	paramPrefix := mustEnv("PARAM_PREFIX")
	maxContextItems := envInt("MAX_CONTEXT_ITEMS", 20)
	maxQuestionLen := envInt("MAX_QUESTION_LENGTH", 300)

	// ---- AWS SDK config ----
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Error("failed to load AWS config", "err", err)
		os.Exit(1)
	}

	// ---- Clients ----
	ssmClient, err := paramstore.New(awsssm.NewFromConfig(cfg))
	if err != nil {
		slog.Error("failed to create SSM client", "err", err)
		os.Exit(1)
	}
	dynamoClient := awsdynamodb.NewFromConfig(cfg)
	stateClient, err := repository.New(dynamoClient, stateTable)
	if err != nil {
		slog.Error("failed to create state client", "err", err)
		os.Exit(1)
	}

	openaiClient, err := openai.NewClient(ssmClient, paramPrefix)
	if err != nil {
		slog.Error("failed to create OpenAI client", "err", err)
		os.Exit(1)
	}

	// ---- Handler ----
	askService, err := usecase.NewAskService(ssmClient, openaiClient, stateClient, paramPrefix, maxContextItems, maxQuestionLen)
	if err != nil {
		slog.Error("failed to create ask service", "err", err)
		os.Exit(1)
	}

	h, err := handler.NewHandler(askService)
	if err != nil {
		slog.Error("failed to create handler", "err", err)
		os.Exit(1)
	}

	lambda.Start(h.Handle)
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable is not set", "key", key)
		os.Exit(1)
	}
	return v
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
