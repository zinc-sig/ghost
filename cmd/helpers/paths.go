package helpers

import "strings"

// ParseOutputPath parses an output path in the format "local[:remote]"
// If no colon is present, returns the path for both local and remote.
// This allows backward compatibility while supporting the new local:remote syntax.
func ParseOutputPath(path string) (local, remote string) {
	parts := strings.SplitN(path, ":", 2)
	if len(parts) == 2 {
		// Explicit local:remote mapping
		local = strings.TrimSpace(parts[0])
		remote = strings.TrimSpace(parts[1])
	} else {
		// No colon: backward compatible mode
		// In this case, we'll use temp files for local and upload to the specified path
		local = "" // Empty local means use temp file
		remote = strings.TrimSpace(path)
	}

	return local, remote
}

// OutputPaths holds the parsed local and remote paths for output files
type OutputPaths struct {
	LocalOutput  string
	RemoteOutput string
	LocalStderr  string
	RemoteStderr string
}

// ParseOutputPaths parses both output and stderr paths
func ParseOutputPaths(outputPath, stderrPath string) OutputPaths {
	localOut, remoteOut := ParseOutputPath(outputPath)
	localErr, remoteErr := ParseOutputPath(stderrPath)

	return OutputPaths{
		LocalOutput:  localOut,
		RemoteOutput: remoteOut,
		LocalStderr:  localErr,
		RemoteStderr: remoteErr,
	}
}

// NeedsTempFiles returns true if temporary files should be created
// This happens when upload is configured but no local path is specified
func (p OutputPaths) NeedsTempFiles(hasUploadProvider bool) bool {
	if !hasUploadProvider {
		return false
	}
	// Need temp files if local path is empty (backward compatible mode)
	return p.LocalOutput == "" || p.LocalStderr == ""
}
