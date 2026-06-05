package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// stubSSM implements ssmParamGetter for tests.
type stubSSM struct {
	value        *string
	err          error
	capturedName string
	capturedDecr bool
}

func (s *stubSSM) GetParameter(_ context.Context, in *ssm.GetParameterInput, _ ...func(*ssm.Options)) (*ssm.GetParameterOutput, error) {
	if in.Name != nil {
		s.capturedName = *in.Name
	}
	if in.WithDecryption != nil {
		s.capturedDecr = *in.WithDecryption
	}
	if s.err != nil {
		return nil, s.err
	}
	return &ssm.GetParameterOutput{Parameter: &types.Parameter{Value: s.value}}, nil
}

// setRequiredEnv sets the minimum env vars needed by resolveDSNWithGetter so
// BuildPasswordDSN passes validation, and returns a cleanup function.
func setRequiredEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	defaults := map[string]string{
		"DB_HOST":              "rds.example.internal",
		"DB_NAME":              "vaultmtg",
		"DB_USER":              "mtga_sync",
		"DB_PORT":              "",
		"DB_PASSWORD_SSM_PATH": "/vaultmtg/app/production/sync-db-password",
	}
	for k, v := range overrides {
		defaults[k] = v
	}
	for k, v := range defaults {
		t.Setenv(k, v)
	}
}

// TestResolveDSNWithGetter_Success verifies that resolveDSNWithGetter reads
// DB_PASSWORD_SSM_PATH from the environment, fetches the password via the SSM
// getter with WithDecryption=true, and assembles a valid DSN.
func TestResolveDSNWithGetter_Success(t *testing.T) {
	setRequiredEnv(t, nil)

	stub := &stubSSM{value: aws.String("s3cr3tp@ss")}
	dsn, err := resolveDSNWithGetter(context.Background(), stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SSM was called with the correct path and decryption enabled.
	if stub.capturedName != "/vaultmtg/app/production/sync-db-password" {
		t.Errorf("SSM path: got %q, want /vaultmtg/app/production/sync-db-password", stub.capturedName)
	}
	if !stub.capturedDecr {
		t.Error("WithDecryption must be true — password is a SecureString")
	}

	// DSN contains the fetched password and required connection fields.
	for _, want := range []string{
		"host=rds.example.internal",
		"dbname=vaultmtg",
		"user=mtga_sync",
		"password=s3cr3tp@ss",
		"sslmode=require",
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN missing %q; got: %s", want, dsn)
		}
	}
}

// TestResolveDSNWithGetter_MissingSSMPath verifies that when DB_PASSWORD_SSM_PATH
// is unset, resolveDSNWithGetter returns a clear error mentioning the missing env var.
func TestResolveDSNWithGetter_MissingSSMPath(t *testing.T) {
	setRequiredEnv(t, map[string]string{"DB_PASSWORD_SSM_PATH": ""})

	stub := &stubSSM{value: aws.String("irrelevant")}
	_, err := resolveDSNWithGetter(context.Background(), stub)
	if err == nil {
		t.Fatal("expected error when DB_PASSWORD_SSM_PATH is empty, got nil")
	}
	if !strings.Contains(err.Error(), "DB_PASSWORD_SSM_PATH") {
		t.Errorf("error must mention DB_PASSWORD_SSM_PATH, got: %v", err)
	}
}

// TestResolveDSNWithGetter_SSMError verifies that an SSM API error surfaces as a
// non-nil error from resolveDSNWithGetter.
func TestResolveDSNWithGetter_SSMError(t *testing.T) {
	setRequiredEnv(t, nil)

	stub := &stubSSM{err: errors.New("AccessDeniedException: no permission")}
	_, err := resolveDSNWithGetter(context.Background(), stub)
	if err == nil {
		t.Fatal("expected error from SSM failure, got nil")
	}
}

// TestResolveDSNWithGetter_SSMEmptyValue verifies that an SSM parameter whose
// value is empty (or nil) surfaces as an error rather than building a DSN with
// an empty password.
func TestResolveDSNWithGetter_SSMEmptyValue(t *testing.T) {
	setRequiredEnv(t, nil)

	stub := &stubSSM{value: nil}
	_, err := resolveDSNWithGetter(context.Background(), stub)
	if err == nil {
		t.Fatal("expected error when SSM parameter has no value, got nil")
	}
}
