package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/zinc-sig/ghost/internal/output"
	"github.com/zinc-sig/ghost/internal/runner"
)

// createJSONResult creates a JSON result from execution results
func createJSONResult(inputPath, outputPath, stderrPath string, result *runner.Result, scoreSet bool, score int) *output.Result {
	jsonResult := &output.Result{
		Command:       result.Command,
		Input:         inputPath,
		Output:        outputPath,
		Stderr:        stderrPath,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
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
func createDiffJSONResult(inputPath, expectedPath, outputPath, stderrPath string, result *runner.Result, scoreSet bool, score int) *output.Result {
	jsonResult := &output.Result{
		Command:       result.Command,
		Input:         inputPath,
		Expected:      &expectedPath,
		Output:        outputPath,
		Stderr:        stderrPath,
		ExitCode:      result.ExitCode,
		ExecutionTime: result.ExecutionTime,
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
