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

### Diff Command

Compare two files and get structured output:

```bash
ghost diff -i actual.txt -e expected.txt -o diff_output.txt
```

Compare with scoring (score applies if files match):

```bash
ghost diff -i actual.txt -e expected.txt -o diff_output.txt --score 100
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

**Note**: The `-i`, `-o`, and `-e` flags are mandatory for all command executions.

## Features

- **I/O Redirection**: Required redirection of stdin, stdout, and stderr to files
- **File Comparison**: Built-in diff command for comparing files with structured output
- **Execution Timing**: Precise execution time measurement in milliseconds
- **Score Tracking**: Optional scoring with conditional logic
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