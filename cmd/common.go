package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/runner"
)

// createJSONResult creates a JSON result from execution results
func createJSONResult(inputPath, outputPath, stderrPath string, result *runner.Result, timeoutMs int64, scoreSet bool, score int) *output.Result {
	jsonResult := &output.Result{
		Command:       result.Command,
		Status:        string(result.Status),
		Input:         inputPath,
		Output:        outputPath,
		Stderr:        stderrPath,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
	}

	// Add timeout if it was set
	if timeoutMs > 0 {
		jsonResult.Timeout = &timeoutMs
	}

	if scoreSet {
		if result.ExitCode == 0 {
			jsonResult.Score = &score
		} else {
			zero := 0
			jsonResult.Score = &zero
		}
	}

	return jsonResult
}

// createDiffJSONResult creates a JSON result for diff command with expected field
func createDiffJSONResult(inputPath, expectedPath, outputPath, stderrPath string, result *runner.Result, timeoutMs int64, scoreSet bool, score int) *output.Result {
	jsonResult := &output.Result{
		Command:       result.Command,
		Status:        string(result.Status),
		Input:         inputPath,
		Expected:      &expectedPath,
		Output:        outputPath,
		Stderr:        stderrPath,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
	}

	// Add timeout if it was set
	if timeoutMs > 0 {
		jsonResult.Timeout = &timeoutMs
	}

	if scoreSet {
		if result.ExitCode == 0 {
			jsonResult.Score = &score
		} else {
			zero := 0
			jsonResult.Score = &zero
		}
	}

	return jsonResult
}

// outputJSON marshals and prints the result as JSON
func outputJSON(result *output.Result) error {
	jsonOutput, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON output: %w", err)
	}

	fmt.Println(string(jsonOutput))
	return nil
}
