# Ghost

A Go CLI command runner for orchestrating command execution with structured output and metadata capture.

## Overview

Ghost is a command orchestration tool that executes commands while capturing execution metadata including exit codes, execution time, and optional scoring. It provides structured JSON output making it ideal for automation, testing, and CI/CD pipelines.

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

Execute a command with input/output redirection:

```bash
ghost run -i input.txt -o output.txt -e stderr.txt -- ./my-command my_args
```

Execute with scoring:

```bash
ghost run --score 85 -o result.txt -- python script.py
```

## JSON Output

Ghost outputs execution results as JSON to stdout:

```json
{
  "input": "input.txt",
  "output": "output.txt", 
  "stderr": "stderr.txt",
  "exit_code": 0,
  "execution_time": 590,
  "score": 85
}
```

## Features

- **I/O Redirection**: Redirect stdin, stdout, and stderr to files
- **Execution Timing**: Precise execution time measurement in milliseconds
- **Score Tracking**: Optional scoring with conditional logic
- **Structured Output**: JSON format for easy parsing and automation
- **Exit Code Capture**: Reliable exit code reporting
- **POSIX Compliant**: Built with Cobra framework for professional CLI experience

## Use Cases

- **Testing Frameworks**: Execute tests with structured result capture
- **CI/CD Pipelines**: Orchestrate build and deployment commands
- **Performance Monitoring**: Track command execution times
- **Process Automation**: Standardized command execution with logging
- **Score-based Systems**: Educational or competitive programming platforms

## License

MIT License - see LICENSE file for details.