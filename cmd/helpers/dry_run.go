package helpers

import (
	"encoding/json"
	"fmt"
	"os"
)

// PrintContextInfo prints context configuration in verbose/dry-run mode
func PrintContextInfo(context any, dryRun bool) {
	if context == nil {
		return
	}

	header := "Context Configuration"
	if dryRun {
		header = "Context Configuration (DRY RUN)"
	}

	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, header)
	fmt.Fprintln(os.Stderr, "========================================")

	// Pretty print the context as JSON
	jsonBytes, err := json.MarshalIndent(context, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "  %v\n", context)
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", string(jsonBytes))
	}

	fmt.Fprintln(os.Stderr, "----------------------------------------")
}