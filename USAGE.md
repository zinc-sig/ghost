# Ghost Usage Guide

## Commands

Ghost provides two main commands:

### Run Command

```
ghost run [flags] -- <command> [args...]
```

The `--` separator is required to distinguish ghost flags from the target command and its arguments.

### Diff Command

```
ghost diff -i <input> -x <expected> -o <output> -e <stderr> [--diff-flags <flags>] [--score <value>]
```

Compare two files and get structured JSON output with execution metadata. You can pass additional flags to the underlying diff command for flexible comparison. All four I/O flags are required, maintaining consistency with the run command.

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
ghost run -i large_dataset.json -o processed.json -e errors.log -- ./data_processor
```

## Diff Command Examples

### Basic File Comparison

Compare two files without scoring:

```bash
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e errors.txt
```

Output (files are identical):
```json
{
  "input": "actual.txt",
  "expected": "expected.txt",
  "output": "diff_output.txt",
  "stderr": "errors.txt",
  "exit_code": 0,
  "execution_time": 5
}
```

Output (files differ):
```json
{
  "input": "actual.txt",
  "expected": "expected.txt",
  "output": "diff_output.txt",
  "stderr": "errors.txt",
  "exit_code": 1,
  "execution_time": 7
}
```

### File Comparison with Scoring

Compare files with score (100 if match, 0 if different):

```bash
ghost diff -i student_output.txt -x solution.txt -o comparison.txt -e errors.txt --score 100
```

### Test Output Validation

```bash
# Compare test output with expected result
ghost diff -i test_output.txt -x expected_output.txt -o test_diff.txt -e errors.txt --score 100

# Check multiple test outputs
for test in tests/*.out; do
  expected="expected/$(basename $test)"
  diff_file="diffs/$(basename $test .out).diff"
  error_file="diffs/$(basename $test .out).err"
  ghost diff -i "$test" -x "$expected" -o "$diff_file" -e "$error_file" --score 100
done
```

### CI/CD Usage

```bash
# Verify configuration file matches template
ghost diff -i config.yml -x config.template.yml -o config_diff.txt -e config_errors.txt

# Compare build artifacts
ghost diff -i build/output.js -x reference/output.js -o build_diff.txt -e build_errors.txt --score 100
```

### Using Diff Flags

The `--diff-flags` option allows you to pass additional flags to the underlying diff command. This is particularly useful for grading scenarios where you want to ignore certain types of differences.

#### Common Grading Flags

- `--ignore-trailing-space` or `-Z`: Ignore white space at line end
- `--ignore-space-change` or `-b`: Ignore changes in the amount of white space
- `--ignore-all-space` or `-w`: Ignore all white space
- `--ignore-blank-lines` or `-B`: Ignore changes where lines are all blank

#### Examples with Diff Flags

Ignore trailing whitespace when comparing:
```bash
ghost diff -i student_output.txt -x solution.txt -o diff.txt -e errors.txt --diff-flags "--ignore-trailing-space"
```

Ignore all whitespace differences:
```bash
ghost diff -i result.txt -x expected.txt -o diff.txt -e errors.txt --diff-flags "-w" --score 100
```

Combine multiple flags to ignore both trailing spaces and blank lines:
```bash
ghost diff -i submission.txt -x answer.txt -o diff.txt -e errors.txt --diff-flags "--ignore-trailing-space --ignore-blank-lines"
```

Use short flags for more concise commands:
```bash
ghost diff -i output.txt -x expected.txt -o diff.txt -e errors.txt --diff-flags "-b -B" --score 100
```

#### Grading Example

For automated grading where formatting shouldn't affect scores:
```bash
# Ignore all formatting differences (spaces and blank lines)
ghost diff -i student.txt -x solution.txt -o feedback.txt -e errors.txt \
  --diff-flags "--ignore-all-space --ignore-blank-lines" \
  --score 100
```

This will give full score (100) if the content matches regardless of spacing differences.

### Notes on Diff Output

- The diff output is written to the file specified by `-o`
- Exit code 0 means files are identical (considering the flags)
- Exit code 1 means files differ
- The actual diff content can be found in the output file
- Score is included only when `--score` flag is used
- When using `--diff-flags`, the comparison respects those flags