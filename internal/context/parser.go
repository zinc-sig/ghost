package context

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strconv"
	"strings"
)

// ParseKV parses a key=value pair, attempting type inference for the value
func ParseKV(kvPair string) (string, any, error) {
	parts := strings.SplitN(kvPair, "=", 2)
	if len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid format, expected key=value: %s", kvPair)
	}

	key := strings.TrimSpace(parts[0])
	if key == "" {
		return "", nil, fmt.Errorf("empty key in key=value pair")
	}

	valueStr := strings.TrimSpace(parts[1])

	// Try to parse as integer first (to avoid "1" being parsed as boolean true)
	if intVal, err := strconv.Atoi(valueStr); err == nil {
		return key, intVal, nil
	}

	// Try to parse as float
	if floatVal, err := strconv.ParseFloat(valueStr, 64); err == nil {
		return key, floatVal, nil
	}

	// Try to parse as boolean (only for explicit "true"/"false" strings)
	if valueStr == "true" || valueStr == "false" {
		boolVal, _ := strconv.ParseBool(valueStr)
		return key, boolVal, nil
	}

	// Return as string
	return key, valueStr, nil
}

// ParseJSON parses a JSON string into a map or other structure
func ParseJSON(jsonStr string) (any, error) {
	var result any
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return result, nil
}

// ParseFile reads and parses JSON from a file
func ParseFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read context file: %w", err)
	}

	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON in file: %w", err)
	}
	return result, nil
}

// ParseEnv parses environment variables with GHOST_CONTEXT prefix
func ParseEnv() map[string]any {
	return ParseEnvWithPrefix("GHOST_CONTEXT")
}

// ParseEnvWithPrefix parses environment variables with a custom prefix
func ParseEnvWithPrefix(prefix string) map[string]any {
	context := make(map[string]any)

	// Check for PREFIX JSON string (e.g., GHOST_CONTEXT or GHOST_UPLOAD_CONFIG)
	if jsonStr := os.Getenv(prefix); jsonStr != "" {
		if parsed, err := ParseJSON(jsonStr); err == nil {
			if m, ok := parsed.(map[string]any); ok {
				maps.Copy(context, m)
			}
		}
	}

	// Check for PREFIX_* variables
	envPrefix := prefix + "_"
	environ := os.Environ()
	for _, env := range environ {
		if strings.HasPrefix(env, envPrefix) {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				key := strings.TrimPrefix(parts[0], envPrefix)
				key = strings.ToLower(key)
				// Apply type inference to env var values
				_, value, _ := ParseKV(key + "=" + parts[1])
				context[key] = value
			}
		}
	}

	if len(context) == 0 {
		return nil
	}
	return context
}

// MergeContexts merges multiple context sources with proper precedence
// Later sources override earlier ones
func MergeContexts(contexts ...any) any {
	result := make(map[string]any)

	for _, ctx := range contexts {
		if ctx == nil {
			continue
		}

		switch v := ctx.(type) {
		case map[string]any:
			maps.Copy(result, v)
		default:
			// If it's not a map, return it as-is (could be array or primitive)
			// This handles cases where --context provides a non-object JSON
			if len(result) == 0 {
				return v
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// BuildContext builds the final context from all sources
func BuildContext(jsonStr string, kvPairs []string, filePath string) (any, error) {
	return BuildContextWithPrefix("GHOST_CONTEXT", jsonStr, kvPairs, filePath)
}

// BuildContextWithPrefix builds context from all sources with a custom environment variable prefix
func BuildContextWithPrefix(envPrefix, jsonStr string, kvPairs []string, filePath string) (any, error) {
	var contexts []any

	// 1. Environment variables (lowest priority)
	if envCtx := ParseEnvWithPrefix(envPrefix); envCtx != nil {
		contexts = append(contexts, envCtx)
	}

	// 2. Context file
	if filePath != "" {
		fileCtx, err := ParseFile(filePath)
		if err != nil {
			return nil, err
		}
		contexts = append(contexts, fileCtx)
	}

	// 3. JSON string
	if jsonStr != "" {
		jsonCtx, err := ParseJSON(jsonStr)
		if err != nil {
			return nil, err
		}
		contexts = append(contexts, jsonCtx)
	}

	// 4. Key-value pairs (highest priority)
	if len(kvPairs) > 0 {
		kvCtx := make(map[string]any)
		for _, kv := range kvPairs {
			key, value, err := ParseKV(kv)
			if err != nil {
				return nil, err
			}
			kvCtx[key] = value
		}
		contexts = append(contexts, kvCtx)
	}

	return MergeContexts(contexts...), nil
}
