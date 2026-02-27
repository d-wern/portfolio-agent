package paramstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// ssmAPI is the minimal AWS SSM interface required by Client.
// *ssm.Client from aws-sdk-go-v2 satisfies this interface.
type ssmAPI interface {
	GetParameter(ctx context.Context, in *ssm.GetParameterInput, optFns ...func(*ssm.Options)) (*ssm.GetParameterOutput, error)
}

// Getter is the interface that wraps GetParameter.
// Consumers (e.g. the OpenAI client) should depend on this interface rather
// than the concrete *Client so they remain testable without real AWS calls.
type Getter interface {
	GetParameter(ctx context.Context, name string) (string, error)
}

// Client wraps an AWS SSM API for parameter retrieval.
type Client struct {
	api ssmAPI
}

// New creates a Client with the given SSM API implementation.
func New(api ssmAPI) (*Client, error) {
	if api == nil {
		return nil, errors.New("paramstore: api must not be nil")
	}
	return &Client{api: api}, nil
}

func (c *Client) GetParameter(ctx context.Context, name string) (string, error) {
	if c.api == nil {
		return "", errors.New("paramstore: client not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("paramstore: name is required")
	}

	withDecryption := true
	out, err := c.api.GetParameter(ctx, &ssm.GetParameterInput{
		Name:           &name,
		WithDecryption: &withDecryption,
	})
	if err != nil {
		return "", fmt.Errorf("paramstore: get parameter %q: %w", name, err)
	}
	if out == nil || out.Parameter == nil || out.Parameter.Value == nil {
		return "", errors.New("paramstore: parameter missing value")
	}
	return *out.Parameter.Value, nil
}
