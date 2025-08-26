# Ghost Usage Guide

## Command Structure

```
ghost run [flags] -- <command> [args...]
```

The `--` separator is required to distinguish ghost flags from the target command and its arguments.

## Flags

### Required Flags

- `-i, --input <file>` - Redirect the specified file to the command's stdin (REQUIRED)
- `-o, --output <file>` - Capture the command's stdout to the specified file (REQUIRED)
- `-e, --stderr <file>` - Capture the command's stderr to the specified file (REQUIRED)

### Optional Flags

- `--score <integer>` - Include a score in the JSON output (conditional on exit code)
- `-h, --help` - Show help information

## Examples

### Basic Command Execution

All commands require I/O redirection flags:

```bash
ghost run -i /dev/null -o output.txt -e error.txt -- echo "Hello, World!"
```

Output:
```json
{
  "input": "/dev/null",
  "output": "output.txt",
  "stderr": "error.txt",
  "exit_code": 0,
  "execution_time": 12
}
```

### With Input/Output Files

Execute a command with full I/O redirection:

```bash
ghost run -i input.txt -o output.txt -e error.log -- ./process-data
```

Output:
```json
{
  "input": "input.txt",
  "output": "output.txt",
  "stderr": "error.log", 
  "exit_code": 0,
  "execution_time": 1250
}
```

### With Scoring (Success Case)

Execute a command with scoring when it succeeds:

```bash
ghost run -i input.txt -o results.txt -e errors.txt --score 95 -- python test_suite.py
```

Output (if exit_code is 0):
```json
{
  "input": "input.txt",
  "output": "results.txt",
  "stderr": "errors.txt",
  "exit_code": 0, 
  "execution_time": 3420,
  "score": 95
}
```

### With Scoring (Failure Case)

Execute the same command when it fails:

Output (if exit_code is non-zero):
```json
{
  "input": "input.txt",
  "output": "results.txt",
  "stderr": "errors.txt",
  "exit_code": 1,
  "execution_time": 890,
  "score": 0
}
```

### Complex Command with Arguments

Execute a command with multiple arguments:

```bash
ghost run -i data.csv -o processed.csv -e errors.log -- python process.py --format csv --verbose
```

## JSON Output Format

Ghost always outputs JSON to stdout with the following structure:

```json
{
  "input": "string",          // Input file path (always present)
  "output": "string",         // Output file path (always present)  
  "stderr": "string",         // Stderr file path (always present)
  "exit_code": 0,             // Command exit code (always present)
  "execution_time": 590,      // Execution time in milliseconds (always present)
  "score": 85                 // Score value (only if --score used)
}
```

### Field Rules

1. **Required Fields**: `input`, `output`, `stderr`, `exit_code` and `execution_time` are always present
2. **File Fields**: `input`, `output`, `stderr` must be specified via their respective flags
3. **Score Field**: Only present if `--score` flag is used
   - If `exit_code` is 0: includes provided score value
   - If `exit_code` is non-zero: score becomes 0

## Exit Codes

- **0**: Ghost executed successfully (target command exit code is captured in JSON)
- **1**: Ghost encountered an error (flag parsing, file access, etc.)
- **2**: Invalid command usage

## Use Cases

### Testing Frameworks

```bash
# Run tests with structured output
ghost run -i /dev/null -o test_results.txt -e test_errors.log --score 100 -- npm test

# Process multiple test files
for file in tests/*.py; do
  ghost run -i "$file" -o "results/$(basename $file .py).out" -e "results/$(basename $file .py).err" -- python test_runner.py
done
```

### CI/CD Integration

```bash
# Build with timing and error capture
ghost run -i /dev/null -o build_output.log -e build_errors.log -- make build

# Deploy with scoring based on success
ghost run -i /dev/null -o deploy.log -e deploy_errors.log --score 100 -- ./deploy.sh production
```

### Performance Monitoring

```bash
# Track execution times for performance analysis
ghost run -i large_dataset.json -o processed.json -- ./data_processor
```