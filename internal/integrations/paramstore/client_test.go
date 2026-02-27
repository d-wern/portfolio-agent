package paramstore

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/stretchr/testify/require"
)

// fakeAPI is a simple fake implementing ssmAPI for tests.
type fakeAPI struct {
	getOut *ssm.GetParameterOutput
	getErr error
}

func (f *fakeAPI) GetParameter(_ context.Context, _ *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	return f.getOut, f.getErr
}

func strPtr(s string) *string { return &s }

func TestGetParameter_HappyPath(t *testing.T) {
	api := &fakeAPI{getOut: &ssm.GetParameterOutput{Parameter: &types.Parameter{
		Name: strPtr("p"), Value: strPtr(`{"k":"v"}`),
	}}}
	client, err := New(api)
	require.NoError(t, err)
	v, err := client.GetParameter(context.Background(), "p")
	require.NoError(t, err)
	require.Equal(t, `{"k":"v"}`, v)
}

func TestGetParameter_HappyPath_SecureString(t *testing.T) {
	typeStr := "SecureString"
	api := &fakeAPI{getOut: &ssm.GetParameterOutput{Parameter: &types.Parameter{
		Name: strPtr("p"), Value: strPtr(`{"k":"v"}`), Type: types.ParameterType(typeStr),
	}}}
	client, err := New(api)
	require.NoError(t, err)
	v, err := client.GetParameter(context.Background(), "p")
	require.NoError(t, err)
	require.Equal(t, `{"k":"v"}`, v)
}

func TestGetParameter_MissingValue(t *testing.T) {
	api := &fakeAPI{getOut: &ssm.GetParameterOutput{Parameter: &types.Parameter{Name: strPtr("p"), Value: nil}}}
	client, err := New(api)
	require.NoError(t, err)
	_, err = client.GetParameter(context.Background(), "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing value")
}

func TestGetParameter_ApiError(t *testing.T) {
	api := &fakeAPI{getErr: errors.New("boom")}
	client, err := New(api)
	require.NoError(t, err)
	_, err = client.GetParameter(context.Background(), "p")
	require.Error(t, err)
	require.ErrorContains(t, err, "boom")
}

func TestGetParameter_ClientNotInitialized(t *testing.T) {
	_, err := (&Client{}).GetParameter(context.Background(), "p")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not initialized")
}

func TestGetParameter_EmptyName(t *testing.T) {
	api := &fakeAPI{}
	client, err := New(api)
	require.NoError(t, err)
	_, err = client.GetParameter(context.Background(), "  ")
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestNew_NilAPI(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be nil")
}
