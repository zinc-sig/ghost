# Ghost

A Go CLI command runner for orchestrating command execution with structured output and metadata capture.

## Overview

Ghost is a command orchestration tool that executes commands while capturing execution metadata including exit codes, execution time, and optional scoring. It provides structured JSON output making it ideal for automation, testing, and CI/CD pipelines.

## Why "Ghost"?

The name draws inspiration from several playful concepts:

- **Ghost in the Shell**: Like the famous anime/manga, Ghost operates at the system level, orchestrating processes and capturing their essence (output) while remaining transparent to the underlying commands.

- **Racing Ghost**: Similar to the ghost racers in Mario Kart that record and replay performances, Ghost captures command outputs and redirects them to different locations, creating a "recording" of your command execution.

- **Silent Observer**: Ghost watches your commands execute without interfering—it observes, captures, and reports, like a friendly specter documenting everything that happens during execution.

In essence, Ghost is your ethereal assistant that seamlessly captures and redirects I/O streams while remaining nearly invisible to the processes it monitors.

## Installation

```bash
go install github.com/zinc-sig/ghost@latest
```

Or build from source:

```bash
git clone https://github.com/zinc-sig/ghost.git
cd ghost
go build -o ghost
```

## Quick Start

### Run Command

Execute a command with required input/output redirection:

```bash
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command my_args
```

Execute with scoring (all I/O flags are required):

```bash
ghost run -i input.txt -o output.txt -e stderr.txt --score 85 -- python script.py
```

With timeout and verbose output:

```bash
ghost run -i input.txt -o output.txt -e stderr.txt --timeout 30s --verbose -- ./slow-command
```

### Diff Command

Compare two files and get structured output:

```bash
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e stderr.txt
```

Compare with scoring (score applies if files match):

```bash
ghost diff -i actual.txt -x expected.txt -o diff_output.txt -e stderr.txt --score 100
```

With custom diff flags for grading:

```bash
ghost diff -i student.txt -x solution.txt -o diff.txt -e stderr.txt --diff-flags "--ignore-trailing-space"
```

## JSON Output

Ghost outputs execution results as JSON to stdout:

```json
{
  "command": "echo hello world",
  "status": "success",
  "input": "input.txt",
  "output": "output.txt", 
  "stderr": "stderr.txt",
  "exit_code": 0,
  "execution_time": 590,
  "timeout": 5000,
  "score": 85
}
```

For diff commands, includes the expected field:

```json
{
  "command": "diff file1.txt file2.txt",
  "status": "failed",
  "input": "file1.txt",
  "expected": "file2.txt",
  "output": "diff_output.txt",
  "stderr": "stderr.txt",
  "exit_code": 1,
  "execution_time": 12
}
```

**Note**: The `-i`, `-o`, and `-e` flags are mandatory for all command executions.

## Features

- **I/O Redirection**: Required redirection of stdin, stdout, and stderr to files
- **File Comparison**: Built-in diff command for comparing files with structured output
- **Execution Timing**: Precise execution time measurement in milliseconds
- **Timeout Support**: Set execution time limits with automatic process termination
- **Status Tracking**: Clear status indicators (success/failed/timeout)
- **Verbose Mode**: Optionally display stderr on terminal while capturing to file
- **Command Logging**: Full command string included in JSON output for auditing
- **Score Tracking**: Optional scoring with conditional logic
- **Diff Flags**: Pass custom flags to diff for flexible comparison (e.g., ignore whitespace)
- **Structured Output**: JSON format for easy parsing and automation
- **Exit Code Capture**: Reliable exit code reporting
- **Auto Directory Creation**: Parent directories are created automatically for output files
- **POSIX Compliant**: Built with Cobra framework for professional CLI experience

## Use Cases

- **Testing Frameworks**: Execute tests with structured result capture
- **CI/CD Pipelines**: Orchestrate build and deployment commands
- **Performance Monitoring**: Track command execution times
- **Process Automation**: Standardized command execution with logging
- **Score-based Systems**: Educational or competitive programming platforms

## License

MIT License - see LICENSE file for details.