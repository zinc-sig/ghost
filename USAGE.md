# Ghost Usage Guide

Comprehensive examples and patterns for using Ghost in various scenarios.

## Table of Contents

- [Command Syntax](#command-syntax)
- [Basic Usage](#basic-usage)
- [Advanced Features](#advanced-features)
- [Common Use Cases](#common-use-cases)
- [JSON Output Reference](#json-output-reference)
- [Exit Codes](#exit-codes)

## Command Syntax

### Run Command

```
ghost run [flags] -- <command> [args...]
```

The `--` separator is **required** to distinguish Ghost flags from the target command and its arguments.

### Diff Command

```
ghost diff -i <input> -x <expected> -o <output> -e <stderr> [flags]
```

All four I/O flags are required for consistency with the run command.

## Basic Usage

### Simple Command Execution

```bash
# Execute a command with minimal setup
ghost run -i /dev/null -o output.txt -e error.txt -- echo "Hello, World!"

# Using actual input file
echo "test data" > input.txt
ghost run -i input.txt -o processed.txt -e errors.log -- cat

# Command with arguments
ghost run -i data.csv -o results.json -e stderr.log -- python process.py --format json --validate
```

### File Comparison

```bash
# Basic diff
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e errors.txt

# Diff with scoring (100 if identical, 0 if different)
ghost diff -i submission.txt -x solution.txt -o feedback.txt -e errors.txt --score 100

# Ignore whitespace differences for grading
ghost diff -i student.txt -x answer.txt -o diff.txt -e errors.txt \
  --diff-flags "--ignore-trailing-space --ignore-blank-lines" \
  --score 100
```

## Advanced Features

### Context Metadata

Attach metadata to track execution details:

```bash
# Simple key-value pairs with type inference
ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-kv "job_id=12345" \
  --context-kv "priority=high" \
  --context-kv "retry_count=3" \
  --context-kv "debug=true" \
  -- ./process-job

# Complex nested structures via JSON
ghost run -i data.json -o result.json -e errors.log \
  --context '{
    "pipeline": {
      "stage": "transform",
      "version": "2.1.0"
    },
    "metrics": {
      "input_size": 1024,
      "expected_duration": 300
    }
  }' \
  -- ./etl-pipeline

# Loading from file
cat > metadata.json << EOF
{
  "experiment": {
    "id": "exp-2024-001",
    "parameters": {
      "learning_rate": 0.001,
      "batch_size": 32
    }
  }
}
EOF

ghost run -i dataset.csv -o model.pkl -e training.log \
  --context-file metadata.json \
  -- python train.py

# Combining multiple sources (precedence: kv > json > file > env)
export GHOST_CONTEXT_ENVIRONMENT=production
export GHOST_CONTEXT_REGION=us-east-1

ghost run -i input.txt -o output.txt -e stderr.txt \
  --context-file base-config.json \
  --context '{"override": "from-json"}' \
  --context-kv "override=from-kv" \
  -- ./app
# Result: override will be "from-kv"
```

### Upload to Storage

Upload outputs to MinIO/S3-compatible storage:

```bash
# Using key-value configuration
ghost run -i /dev/null -o results/test-output.txt -e results/test-errors.txt \
  --upload-provider minio \
  --upload-config-kv "endpoint=minio.internal:9000" \
  --upload-config-kv "access_key=$MINIO_ACCESS_KEY" \
  --upload-config-kv "secret_key=$MINIO_SECRET_KEY" \
  --upload-config-kv "bucket=test-results" \
  --upload-config-kv "prefix=$(date +%Y-%m-%d)/" \
  -- ./run-tests.sh

# Using JSON configuration file
cat > s3-config.json << EOF
{
  "endpoint": "s3.amazonaws.com",
  "access_key": "${AWS_ACCESS_KEY_ID}",
  "secret_key": "${AWS_SECRET_ACCESS_KEY}",
  "bucket": "my-app-artifacts",
  "region": "us-west-2",
  "prefix": "builds/",
  "use_ssl": true
}
EOF

ghost run -i /dev/null -o build.log -e build-errors.log \
  --upload-provider minio \
  --upload-config-file s3-config.json \
  -- make build

# Files are uploaded to specified paths after execution completes
```

### Webhook Integration

Send results to external systems with retry logic:

```bash
# Basic webhook notification
ghost run -i test.txt -o result.txt -e error.txt \
  --webhook-url https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXX \
  -- ./critical-process

# With authentication and custom settings
ghost run -i input.txt -o output.txt -e stderr.txt \
  --webhook-url https://api.monitoring.com/v1/events \
  --webhook-method POST \
  --webhook-auth-type bearer \
  --webhook-auth-token "$API_TOKEN" \
  --webhook-retries 5 \
  --webhook-retry-delay 2s \
  --webhook-timeout 60s \
  -- ./long-running-job

# Webhook with upload and context (complete integration)
ghost run -i batch.csv -o processed.csv -e processing.log \
  --upload-provider minio \
  --upload-config-kv "endpoint=storage.local:9000" \
  --upload-config-kv "bucket=outputs" \
  --webhook-url https://api.pipeline.com/notify \
  --webhook-auth-type api-key \
  --webhook-auth-token "$PIPELINE_API_KEY" \
  --context-kv "batch_id=$(uuidgen)" \
  --context-kv "processor_version=3.2.1" \
  -- python batch_processor.py
```

### Timeout and Verbose Mode

```bash
# Set execution timeout
ghost run -i large-dataset.csv -o analysis.json -e errors.log \
  --timeout 5m \
  -- python heavy_analysis.py

# Verbose mode: see stderr on terminal while also capturing to file
ghost run -i config.yml -o deployment.log -e deployment-errors.log \
  --verbose \
  -- kubectl apply -f config.yml

# Combine timeout with verbose for debugging
ghost run -i test-suite.txt -o test-results.xml -e test-errors.log \
  --timeout 30s \
  --verbose \
  -- npm test
```

## Common Use Cases

### Automated Testing & Grading

```bash
# Grade student submissions with tolerance for formatting
for submission in submissions/*.c; do
  student_id=$(basename "$submission" .c)
  
  # Compile and upload the binary
  ghost run -i /dev/null -o "results/${student_id}_compile.log" \
    -e "results/${student_id}_compile_errors.log" \
    --upload-files "/tmp/${student_id}:binaries/${student_id}" \
    --context-kv "student_id=${student_id}" \
    --context-kv "phase=compilation" \
    --timeout 10s \
    -- gcc -o "/tmp/${student_id}" "$submission"
  
  # Run tests if compilation succeeded
  if [ $? -eq 0 ]; then
    ghost run -i test_input.txt -o "results/${student_id}_output.txt" \
      -e "results/${student_id}_runtime_errors.log" \
      --context-kv "student_id=${student_id}" \
      --context-kv "phase=execution" \
      --timeout 5s \
      -- "/tmp/${student_id}"
    
    # Compare output with expected
    ghost diff -i "results/${student_id}_output.txt" -x expected_output.txt \
      -o "results/${student_id}_diff.txt" -e "results/${student_id}_diff_errors.log" \
      --diff-flags "--ignore-trailing-space --ignore-blank-lines" \
      --score 100 \
      --context-kv "student_id=${student_id}" \
      --context-kv "phase=grading"
  fi
done
```

### CI/CD Pipeline Integration

```bash
#!/bin/bash
# ci-pipeline.sh

# Build stage with artifact upload and local copies
ghost run -i /dev/null -o local/build.log:logs/build.log -e local/errors.log:logs/errors.log \
  --upload-provider minio \
  --upload-config-kv "bucket=ci-artifacts" \
  --upload-config-kv "prefix=builds/${BUILD_ID}/" \
  --upload-files "app.exe:binaries/app.exe" \
  --upload-files "app.pdb:debug/app.pdb" \
  --context-kv "stage=build" \
  --context-kv "commit=${GIT_COMMIT}" \
  --context-kv "branch=${GIT_BRANCH}" \
  --timeout 10m \
  --webhook-url "${CI_WEBHOOK_URL}" \
  -- make build

# Test stage (only if build succeeds)
if [ $? -eq 0 ]; then
  ghost run -i /dev/null -o test-results.xml -e test-errors.log \
    --context-kv "stage=test" \
    --context-kv "commit=${GIT_COMMIT}" \
    --score 100 \
    --timeout 15m \
    --webhook-url "${CI_WEBHOOK_URL}" \
    -- make test
fi

# Deploy stage (only if tests pass)
if [ $? -eq 0 ]; then
  ghost run -i /dev/null -o deploy.log -e deploy-errors.log \
    --context-kv "stage=deploy" \
    --context-kv "environment=${DEPLOY_ENV}" \
    --context-kv "version=${VERSION}" \
    --upload-provider minio \
    --upload-config-file deploy-s3.json \
    --webhook-url "${DEPLOY_WEBHOOK_URL}" \
    --webhook-auth-type bearer \
    --webhook-auth-token "${DEPLOY_TOKEN}" \
    -- ./deploy.sh
fi
```

### Data Processing with Additional Files Upload

```bash
# Process data and upload generated reports/artifacts
ghost run -i raw_data.csv -o processing.log -e errors.log \
  --upload-provider minio \
  --upload-config-kv "bucket=data-lake" \
  --upload-config-kv "prefix=processed/$(date +%Y%m%d)/" \
  --upload-files "summary_report.pdf" \
  --upload-files "cleaned_data.csv:data/cleaned.csv" \
  --upload-files "visualization.png:images/viz.png" \
  --upload-files "statistics.json:meta/stats.json" \
  -- python process_data.py raw_data.csv

# Compile and upload binary with debug symbols
ghost run -i /dev/null -o compile.log -e compile_errors.log \
  --upload-provider minio \
  --upload-config-kv "bucket=builds" \
  --upload-files "program:bin/program" \
  --upload-files "program.map:debug/program.map" \
  --upload-files "program.pdb:debug/program.pdb" \
  -- gcc -g -o program main.c -Wl,-Map=program.map
```

### Performance Benchmarking

```bash
# Run benchmarks and collect timing data
for size in 100 1000 10000 100000; do
  ghost run -i "data_${size}.json" -o "bench_${size}.out" -e "bench_${size}.err" \
    --context-kv "input_size=${size}" \
    --context-kv "algorithm=quicksort" \
    --context-kv "timestamp=$(date -Iseconds)" \
    --timeout 1m \
    -- ./benchmark --size "${size}"
done

# Parse execution times from JSON output
for size in 100 1000 10000 100000; do
  echo "Size ${size}: $(ghost run ... | jq -r '.execution_time')ms"
done
```

### Batch Processing with Notifications

```bash
# Process multiple files with webhook notifications
find ./incoming -name "*.xml" | while read -r file; do
  basename_no_ext=$(basename "$file" .xml)
  
  ghost run -i "$file" -o "processed/${basename_no_ext}.json" \
    -e "errors/${basename_no_ext}.log" \
    --context-kv "source_file=${file}" \
    --context-kv "process_time=$(date -Iseconds)" \
    --upload-provider minio \
    --upload-config-kv "endpoint=storage:9000" \
    --upload-config-kv "bucket=processed-data" \
    --webhook-url https://api.monitoring.com/batch-status \
    --webhook-auth-type api-key \
    --webhook-auth-token "${MONITORING_API_KEY}" \
    -- python xml_to_json.py
    
  # Move processed file
  [ $? -eq 0 ] && mv "$file" ./completed/
done
```

## JSON Output Reference

### Standard Output Structure

```json
{
  "command": "echo Hello World",           // Always present
  "status": "success",                     // success | failed | timeout
  "input": "/dev/null",                    // Always present
  "output": "output.txt",                  // Always present
  "stderr": "stderr.txt",                  // Always present
  "exit_code": 0,                         // -1 for timeout
  "execution_time": 125,                  // Milliseconds
  "timeout": 30000,                       // Only if --timeout used
  "score": 85,                            // Only if --score used
  "context": {                            // Only if context provided
    "user_id": 123,
    "test_case": "integration_01"
  },
  "webhook_sent": true,                   // Only if webhook configured
  "webhook_error": ""                     // Empty on success
}
```

### Diff Command Output

```json
{
  "command": "diff actual.txt expected.txt",
  "status": "failed",                     // failed = files differ
  "input": "actual.txt",
  "expected": "expected.txt",             // Only in diff output
  "output": "diff.txt",
  "stderr": "errors.txt",
  "exit_code": 1,                        // 0 = identical, 1 = different
  "execution_time": 8,
  "score": 0                              // 0 because files differ
}
```

### Parsing Output Examples

```bash
# Extract exit code
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command | jq -r '.exit_code'

# Check if command succeeded
if [ "$(ghost run ... | jq -r '.status')" = "success" ]; then
  echo "Command succeeded"
fi

# Get execution time in seconds
ghost run ... | jq -r '.execution_time / 1000'

# Extract context data
ghost run ... | jq -r '.context.user_id'

# Check webhook status
ghost run ... | jq -r 'if .webhook_sent then "Webhook sent" else "Webhook failed: " + .webhook_error end'
```

## Exit Codes

Ghost itself uses the following exit codes:

- **0**: Ghost executed successfully (target command exit code is in JSON)
- **1**: Ghost encountered an error (invalid flags, file access issues, etc.)
- **2**: Invalid command usage (missing required flags, no command specified)

The target command's exit code is captured in the JSON output's `exit_code` field.

## Tips and Best Practices

1. **Always use absolute paths** when running Ghost in scripts to avoid path resolution issues

2. **Check Ghost's exit code first**, then parse JSON for the target command's result:
   ```bash
   output=$(ghost run -i in.txt -o out.txt -e err.txt -- ./cmd)
   if [ $? -ne 0 ]; then
     echo "Ghost failed to execute"
     exit 1
   fi
   
   exit_code=$(echo "$output" | jq -r '.exit_code')
   if [ "$exit_code" -ne 0 ]; then
     echo "Command failed with exit code: $exit_code"
   fi
   ```

3. **Use context for debugging** - include relevant metadata that helps troubleshoot issues:
   ```bash
   --context-kv "hostname=$(hostname)" \
   --context-kv "user=$USER" \
   --context-kv "pwd=$(pwd)"
   ```

4. **Set appropriate timeouts** for long-running commands to prevent hanging:
   ```bash
   --timeout 5m  # Generous timeout for compilation
   --timeout 30s # Reasonable timeout for tests
   ```

5. **Use verbose mode during development**, disable in production:
   ```bash
   [ "$DEBUG" = "true" ] && VERBOSE_FLAG="-v" || VERBOSE_FLAG=""
   ghost run -i in.txt -o out.txt -e err.txt $VERBOSE_FLAG -- ./cmd
   ```

6. **Store webhook credentials securely** using environment variables or secret management:
   ```bash
   export GHOST_WEBHOOK_AUTH_TOKEN=$(vault read -field=token secret/webhook)
   ```

7. **Use diff flags for grading** to ignore insignificant differences:
   ```bash
   --diff-flags "--ignore-all-space --ignore-blank-lines"
   ```

8. **Batch webhook notifications** to avoid overwhelming endpoints during bulk operations

9. **Test upload configuration** with small files before processing large datasets

10. **Parse JSON output safely** using proper tools like `jq` instead of regex

## See Also

- [Configuration Reference](CONFIG.md) - Complete list of flags and environment variables
- [README](README.md) - Quick start guide
- [Developer Notes](CLAUDE.md) - Implementation details and development guidance