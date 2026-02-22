package handler

import (
	"context"
	"encoding/json"
)

type Response struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

func Handler(_ context.Context, raw json.RawMessage) (Response, error) {
	return Response{
		StatusCode: 200,
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       "Hello, World!",
	}, nil
}
