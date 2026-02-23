package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	sdk "github.com/getcreddy/creddy-plugin-sdk"
)

const (
	PluginName    = "aws"
	PluginVersion = "0.1.0"
)

// AWSPlugin implements the Creddy Plugin interface for AWS
type AWSPlugin struct {
	config *AWSConfig
}

// AWSConfig contains the plugin configuration
type AWSConfig struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	RoleARN         string `json:"role_arn"`
	Region          string `json:"region,omitempty"`
	ExternalID      string `json:"external_id,omitempty"`
}

// AWSCredentialValue is the JSON structure returned as the credential value
type AWSCredentialValue struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
	SessionToken    string `json:"session_token"`
	Region          string `json:"region"`
}

func (p *AWSPlugin) Info(ctx context.Context) (*sdk.PluginInfo, error) {
	return &sdk.PluginInfo{
		Name:             PluginName,
		Version:          PluginVersion,
		Description:      "AWS STS temporary credentials via AssumeRole",
		MinCreddyVersion: "0.4.0",
	}, nil
}

func (p *AWSPlugin) Scopes(ctx context.Context) ([]sdk.ScopeSpec, error) {
	return []sdk.ScopeSpec{
		{
			Pattern:     "aws",
			Description: "Full AWS access using the configured role",
			Examples:    []string{"aws"},
		},
		{
			Pattern:     "aws:s3",
			Description: "AWS S3 access (logical scope - actual permissions depend on role)",
			Examples:    []string{"aws:s3"},
		},
		{
			Pattern:     "aws:bedrock",
			Description: "AWS Bedrock access (logical scope - actual permissions depend on role)",
			Examples:    []string{"aws:bedrock"},
		},
		{
			Pattern:     "aws:lambda",
			Description: "AWS Lambda access (logical scope - actual permissions depend on role)",
			Examples:    []string{"aws:lambda"},
		},
		{
			Pattern:     "aws:ecr",
			Description: "AWS ECR access (logical scope - actual permissions depend on role)",
			Examples:    []string{"aws:ecr"},
		},
	}, nil
}

func (p *AWSPlugin) Configure(ctx context.Context, configJSON string) error {
	var cfg AWSConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return fmt.Errorf("invalid config JSON: %w", err)
	}

	if cfg.AccessKeyID == "" {
		return fmt.Errorf("access_key_id is required")
	}
	if cfg.SecretAccessKey == "" {
		return fmt.Errorf("secret_access_key is required")
	}
	if cfg.RoleARN == "" {
		return fmt.Errorf("role_arn is required")
	}

	// Default region
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}

	p.config = &cfg
	return nil
}

func (p *AWSPlugin) Validate(ctx context.Context) error {
	if p.config == nil {
		return fmt.Errorf("plugin not configured")
	}

	// Try to get caller identity to validate credentials
	client, err := p.createSTSClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create STS client: %w", err)
	}

	_, err = client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("failed to validate AWS credentials: %w", err)
	}

	return nil
}

func (p *AWSPlugin) GetCredential(ctx context.Context, req *sdk.CredentialRequest) (*sdk.Credential, error) {
	if p.config == nil {
		return nil, fmt.Errorf("plugin not configured")
	}

	// Validate the scope
	if !isValidAWSScope(req.Scope) {
		return nil, fmt.Errorf("invalid aws scope: %s", req.Scope)
	}

	// Create STS client
	client, err := p.createSTSClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create STS client: %w", err)
	}

	// Calculate session duration (default 1 hour, max from TTL if provided)
	sessionDuration := int32(3600) // 1 hour default
	if req.TTL > 0 {
		ttlSeconds := int32(req.TTL.Seconds())
		// AWS allows 900 to 43200 seconds (15 min to 12 hours)
		if ttlSeconds >= 900 && ttlSeconds <= 43200 {
			sessionDuration = ttlSeconds
		} else if ttlSeconds < 900 {
			sessionDuration = 900 // minimum
		} else {
			sessionDuration = 43200 // maximum
		}
	}

	// Build assume role input
	assumeInput := &sts.AssumeRoleInput{
		RoleArn:         aws.String(p.config.RoleARN),
		RoleSessionName: aws.String(fmt.Sprintf("creddy-%s-%d", req.Scope, time.Now().Unix())),
		DurationSeconds: aws.Int32(sessionDuration),
	}

	if p.config.ExternalID != "" {
		assumeInput.ExternalId = aws.String(p.config.ExternalID)
	}

	// Assume the role
	result, err := client.AssumeRole(ctx, assumeInput)
	if err != nil {
		return nil, fmt.Errorf("failed to assume role: %w", err)
	}

	// Build the credential value as JSON
	credValue := AWSCredentialValue{
		AccessKeyID:     *result.Credentials.AccessKeyId,
		SecretAccessKey: *result.Credentials.SecretAccessKey,
		SessionToken:    *result.Credentials.SessionToken,
		Region:          p.config.Region,
	}

	credJSON, err := json.Marshal(credValue)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal credential: %w", err)
	}

	return &sdk.Credential{
		Value:     string(credJSON),
		ExpiresAt: *result.Credentials.Expiration,
		Metadata: map[string]string{
			"role_arn": p.config.RoleARN,
			"region":   p.config.Region,
			"scope":    req.Scope,
		},
	}, nil
}

func (p *AWSPlugin) RevokeCredential(ctx context.Context, externalID string) error {
	// STS temporary credentials cannot be revoked
	// They expire automatically based on the session duration
	return nil
}

func (p *AWSPlugin) MatchScope(ctx context.Context, scope string) (bool, error) {
	return isValidAWSScope(scope), nil
}

// --- AWS helpers ---

func (p *AWSPlugin) createSTSClient(ctx context.Context) (*sts.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(p.config.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			p.config.AccessKeyID,
			p.config.SecretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, err
	}

	return sts.NewFromConfig(cfg), nil
}

// isValidAWSScope checks if a scope is a valid AWS scope
func isValidAWSScope(scope string) bool {
	validScopes := map[string]bool{
		"aws":         true,
		"aws:s3":      true,
		"aws:bedrock": true,
		"aws:lambda":  true,
		"aws:ecr":     true,
	}

	// Check exact match
	if validScopes[scope] {
		return true
	}

	// Check if it starts with "aws:" for extensibility
	if strings.HasPrefix(scope, "aws:") {
		return true
	}

	return false
}
