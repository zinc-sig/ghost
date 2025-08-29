# Configuration Reference

Complete reference for all Ghost configuration options including command-line flags and environment variables.

## Command-Line Flags

### Core Flags (Both Commands)

| Flag | Short | Description | Required | Default |
|------|-------|-------------|----------|---------|
| `--input` | `-i` | Input file to redirect to stdin | ✅ Yes | - |
| `--output` | `-o` | Output file to capture stdout | ✅ Yes | - |
| `--stderr` | `-e` | Error file to capture stderr | ✅ Yes | - |
| `--verbose` | `-v` | Show stderr on terminal while capturing | No | `false` |
| `--timeout` | `-t` | Execution timeout (e.g., 30s, 2m, 500ms) | No | - |
| `--score` | - | Optional score (0 if command fails) | No | - |
| `--help` | `-h` | Show help information | No | - |

### Diff-Specific Flags

| Flag | Short | Description | Required | Default |
|------|-------|-------------|----------|---------|
| `--expected` | `-x` | Expected file to compare against | ✅ Yes | - |
| `--diff-flags` | - | Flags to pass to diff command | No | - |

Common diff flags for grading:
- `--ignore-trailing-space` or `-Z`: Ignore white space at line end
- `--ignore-space-change` or `-b`: Ignore changes in amount of white space
- `--ignore-all-space` or `-w`: Ignore all white space
- `--ignore-blank-lines` or `-B`: Ignore blank line changes

### Context Configuration Flags

| Flag | Description | Example |
|------|-------------|---------|
| `--context` | Context data as JSON string | `'{"user": "alice", "env": "prod"}'` |
| `--context-kv` | Key=value pairs (repeatable) | `"user_id=123" "score=95.5"` |
| `--context-file` | Path to JSON file | `metadata.json` |

### Upload Configuration Flags

| Flag | Description | Example |
|------|-------------|---------|
| `--upload-provider` | Provider type | `minio` |
| `--upload-config` | Configuration as JSON | `'{"endpoint": "localhost:9000"}'` |
| `--upload-config-kv` | Config key=value pairs (repeatable) | `"bucket=results"` |
| `--upload-config-file` | Path to config JSON file | `upload-config.json` |
| `--upload-files` | Additional files to upload (repeatable) | `"output.bin"` or `"local.txt:remote/path.txt"` |

### Webhook Configuration Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--webhook-url` | Webhook endpoint URL | - |
| `--webhook-method` | HTTP method (GET, POST, PUT, PATCH, DELETE) | `POST` |
| `--webhook-auth-type` | Authentication type (none, bearer, api-key) | `none` |
| `--webhook-auth-token` | Authentication token | - |
| `--webhook-retries` | Maximum retry attempts (0 = no retries) | `3` |
| `--webhook-retry-delay` | Initial delay between retries | `1s` |
| `--webhook-timeout` | Request timeout duration | `30s` |
| `--webhook-config` | Configuration as JSON | - |
| `--webhook-config-kv` | Config key=value pairs (repeatable) | - |
| `--webhook-config-file` | Path to config JSON file | - |

## Environment Variables

### Context Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `GHOST_CONTEXT` | JSON object for context data | `{"env": "production"}` |
| `GHOST_CONTEXT_*` | Individual context keys (lowercased) | `GHOST_CONTEXT_USER_ID=123` |

**Type Inference**: Values in `GHOST_CONTEXT_*` variables are automatically converted:
- Numbers: `"123"` → `123`, `"3.14"` → `3.14`
- Booleans: `"true"` → `true`, `"false"` → `false`
- Strings: Everything else remains as strings

### Upload Configuration Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `GHOST_UPLOAD_CONFIG_ENDPOINT` | MinIO/S3 endpoint | `localhost:9000` |
| `GHOST_UPLOAD_CONFIG_ACCESS_KEY` | Access key | `minioadmin` |
| `GHOST_UPLOAD_CONFIG_SECRET_KEY` | Secret key | `minioadmin` |
| `GHOST_UPLOAD_CONFIG_BUCKET` | Target bucket | `ghost-results` |
| `GHOST_UPLOAD_CONFIG_PREFIX` | Path prefix | `tests/` |
| `GHOST_UPLOAD_CONFIG_USE_SSL` | Enable SSL | `true` |
| `GHOST_UPLOAD_CONFIG_*` | Any other MinIO/S3 option | Various |

### Webhook Configuration Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GHOST_WEBHOOK_URL` | Webhook endpoint | - |
| `GHOST_WEBHOOK_METHOD` | HTTP method | `POST` |
| `GHOST_WEBHOOK_AUTH_TYPE` | Auth type (none, bearer, api-key) | `none` |
| `GHOST_WEBHOOK_AUTH_TOKEN` | Auth token | - |
| `GHOST_WEBHOOK_RETRIES` | Max retry attempts | `3` |
| `GHOST_WEBHOOK_RETRY_DELAY` | Initial retry delay | `1s` |
| `GHOST_WEBHOOK_TIMEOUT` | Request timeout | `30s` |
| `GHOST_WEBHOOK_*` | Any other webhook option | Various |

## Configuration Precedence

When the same configuration key appears in multiple sources, the precedence order is:

1. **Direct flags** (highest priority) - e.g., `--webhook-url`
2. **Key-value pairs** - e.g., `--webhook-config-kv "url=..."`
3. **JSON string** - e.g., `--webhook-config '{"url": "..."}'`
4. **Config file** - e.g., `--webhook-config-file config.json`
5. **Environment variables** (lowest priority) - e.g., `GHOST_WEBHOOK_URL`

### Example: Multiple Configuration Sources

```bash
# Environment variable (lowest priority)
export GHOST_WEBHOOK_URL=https://default.example.com

# Config file (webhook-config.json)
{
  "url": "https://file.example.com",
  "retries": 5
}

# Command with multiple sources
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-config-file webhook-config.json \       # Sets URL to file.example.com
  --webhook-config '{"url": "https://json.example.com"}' \  # Overrides to json.example.com
  --webhook-config-kv "url=https://kv.example.com" \       # Overrides to kv.example.com
  --webhook-url https://flag.example.com \                 # Final override to flag.example.com
  -- ./my-command

# Result: URL will be https://flag.example.com
```

## Provider-Specific Configuration

### MinIO/S3 Upload Configuration

Required fields:
- `endpoint`: MinIO/S3 endpoint (without protocol)
- `access_key`: Access key for authentication
- `secret_key`: Secret key for authentication
- `bucket`: Target bucket name

Optional fields:
- `prefix`: Path prefix for uploaded files
- `use_ssl`: Enable SSL/TLS (default: true for S3, false for localhost)
- `region`: AWS region (for S3)

#### Additional Files Upload

The `--upload-files` flag allows uploading files created by your command alongside the standard output/stderr files.

Format: `local_path[:remote_path]`
- If remote path is omitted, the local path is used as the remote path
- Can be specified multiple times for multiple files
- Files are validated to exist after command execution
- All uploads respect the configured prefix

Examples:
```bash
# Upload with same local/remote path
--upload-files "output.bin"

# Upload with different remote path  
--upload-files "local.txt:remote/path.txt"

# Multiple files
--upload-files "result1.csv" \
--upload-files "result2.csv:data/result2.csv" \
--upload-files "summary.json:reports/summary.json"
```

Example configuration:
```json
{
  "endpoint": "s3.amazonaws.com",
  "access_key": "AKIAIOSFODNN7EXAMPLE",
  "secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  "bucket": "my-bucket",
  "prefix": "ghost-outputs/",
  "region": "us-east-1",
  "use_ssl": true
}
```

### Webhook Retry Behavior

The webhook client implements exponential backoff with the following behavior:

- **Initial delay**: Configured via `retry_delay` (default: 1s)
- **Backoff multiplier**: 2.0 (doubles each retry)
- **Max delay**: 30 seconds (caps the retry delay)
- **Retryable status codes**: 408, 425, 429, 500, 502, 503, 504

Example retry sequence with defaults:
1. First retry: 1 second delay
2. Second retry: 2 seconds delay
3. Third retry: 4 seconds delay
4. Fourth retry: 8 seconds delay
5. Fifth retry: 16 seconds delay
6. Sixth+ retry: 30 seconds delay (capped)

## JSON Output Fields

### Always Present Fields

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | Full command that was executed |
| `status` | string | Execution status: "success", "failed", or "timeout" |
| `input` | string | Input file path |
| `output` | string | Output file path |
| `stderr` | string | Stderr file path |
| `exit_code` | integer | Process exit code (-1 for timeout) |
| `execution_time` | integer | Execution time in milliseconds |

### Optional Fields

| Field | Type | When Present |
|-------|------|--------------|
| `expected` | string | Only in diff command output |
| `timeout` | integer | When `--timeout` flag is used (milliseconds) |
| `score` | integer | When `--score` flag is used |
| `context` | object/any | When context is provided via any method |
| `webhook_sent` | boolean | When webhook is configured |
| `webhook_error` | string | When webhook fails (empty on success) |

## Configuration Examples

### Full Context Configuration

```bash
# Using all context input methods with precedence
export GHOST_CONTEXT='{"env": "production"}'
export GHOST_CONTEXT_BUILD_ID=123

echo '{"version": "1.0.0"}' > context.json

ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-file context.json \
  --context '{"feature": "auth"}' \
  --context-kv "user=alice" \
  --context-kv "priority=high" \
  -- ./my-app

# Resulting context:
# {
#   "env": "production",     # from environment
#   "build_id": 123,         # from environment
#   "version": "1.0.0",      # from file
#   "feature": "auth",       # from JSON flag
#   "user": "alice",         # from key-value
#   "priority": "high"       # from key-value
# }
```

### Complete Webhook Configuration

```bash
# Configure webhook with all options
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.example.com/webhooks/ghost \
  --webhook-method POST \
  --webhook-auth-type bearer \
  --webhook-auth-token "eyJhbGciOiJIUzI1NiIs..." \
  --webhook-retries 5 \
  --webhook-retry-delay 2s \
  --webhook-timeout 45s \
  -- ./critical-process
```

### MinIO Upload with Environment Variables

```bash
# Set up MinIO configuration
export GHOST_UPLOAD_CONFIG_ENDPOINT=minio.example.com
export GHOST_UPLOAD_CONFIG_ACCESS_KEY=myaccesskey
export GHOST_UPLOAD_CONFIG_SECRET_KEY=mysecretkey
export GHOST_UPLOAD_CONFIG_BUCKET=results
export GHOST_UPLOAD_CONFIG_PREFIX=ci-builds/
export GHOST_UPLOAD_CONFIG_USE_SSL=true

# Run with upload enabled
ghost run -i test.txt -o output.txt -e stderr.txt \
  --upload-provider minio \
  -- ./run-tests

# Files will be uploaded to:
# minio.example.com/results/ci-builds/output.txt
# minio.example.com/results/ci-builds/stderr.txt
```