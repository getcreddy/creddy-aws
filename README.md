# creddy-aws

Creddy plugin for AWS STS temporary credentials.

## Overview

This plugin issues ephemeral AWS credentials using STS AssumeRole. Credentials are temporary and expire automatically, providing secure access without long-lived keys.

## Installation

```bash
creddy plugin install aws
```

Or build from source:

```bash
make build
make install  # copies to ~/.creddy/plugins/
```

## Configuration

Add the AWS backend to Creddy:

```bash
creddy backend add aws \
  --access-key-id AKIAIOSFODNN7EXAMPLE \
  --secret-access-key wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY \
  --role-arn arn:aws:iam::123456789012:role/MyRole \
  --region us-east-1
```

### Required Settings

| Setting | Description |
|---------|-------------|
| `access_key_id` | AWS access key ID for the IAM user that will assume the role |
| `secret_access_key` | AWS secret access key |
| `role_arn` | ARN of the IAM role to assume |

### Optional Settings

| Setting | Description | Default |
|---------|-------------|---------|
| `region` | AWS region | `us-east-1` |
| `external_id` | External ID for role assumption (if required by trust policy) | |

## Scopes

| Pattern | Description |
|---------|-------------|
| `aws` | Full AWS access using the configured role |
| `aws:s3` | S3 access (logical scope - permissions depend on role) |
| `aws:bedrock` | Bedrock access (logical scope - permissions depend on role) |
| `aws:lambda` | Lambda access (logical scope - permissions depend on role) |
| `aws:ecr` | ECR access (logical scope - permissions depend on role) |

**Note:** Scopes are logical identifiers. Actual permissions are determined by the IAM role's policies. All scopes return credentials with the same role permissions.

## Usage

```bash
# Get credentials for general AWS access
creddy get aws --scope "aws"

# Get credentials for S3 operations
creddy get aws --scope "aws:s3"

# Get credentials with custom TTL (15 min to 12 hours)
creddy get aws --scope "aws:bedrock" --ttl 2h
```

### Using the Credentials

The credential value is a JSON object:

```json
{
  "access_key_id": "ASIAXXX...",
  "secret_access_key": "xxx...",
  "session_token": "xxx...",
  "region": "us-east-1"
}
```

To use with AWS CLI:

```bash
export AWS_ACCESS_KEY_ID="..."
export AWS_SECRET_ACCESS_KEY="..."
export AWS_SESSION_TOKEN="..."
export AWS_REGION="..."
```

## Development

### Standalone Testing

The plugin can run standalone for testing without Creddy:

```bash
# Build
make build

# Show plugin info
make info

# List supported scopes
make scopes

# Create a test config
cat > test-config.json << 'EOF'
{
  "access_key_id": "AKIAIOSFODNN7EXAMPLE",
  "secret_access_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  "role_arn": "arn:aws:iam::123456789012:role/MyRole",
  "region": "us-east-1"
}
EOF

# Validate configuration
make validate CONFIG=test-config.json

# Get a credential
make get CONFIG=test-config.json SCOPE="aws:s3"
```

### Dev Mode

Auto-rebuild and install on file changes:

```bash
make dev
```

### Testing

```bash
make test
```

## How It Works

1. Plugin uses configured IAM credentials to call STS AssumeRole
2. STS returns temporary credentials (access key, secret key, session token)
3. Credentials are valid for the requested duration (default 1 hour)
4. Credentials expire automatically - no revocation needed

## IAM Setup

### IAM User (for plugin)

Create an IAM user with permission to assume the target role:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "sts:AssumeRole",
      "Resource": "arn:aws:iam::123456789012:role/MyRole"
    }
  ]
}
```

### IAM Role (to be assumed)

Create an IAM role with a trust policy allowing the IAM user:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::123456789012:user/creddy-user"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
```

Optionally require an external ID:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::123456789012:user/creddy-user"
      },
      "Action": "sts:AssumeRole",
      "Condition": {
        "StringEquals": {
          "sts:ExternalId": "your-external-id"
        }
      }
    }
  ]
}
```

## License

Apache 2.0
