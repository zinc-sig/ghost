package context

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseKV(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantKey   string
		wantValue any
		wantErr   bool
	}{
		{
			name:      "simple string",
			input:     "name=Alice",
			wantKey:   "name",
			wantValue: "Alice",
			wantErr:   false,
		},
		{
			name:      "integer value",
			input:     "age=25",
			wantKey:   "age",
			wantValue: 25,
			wantErr:   false,
		},
		{
			name:      "float value",
			input:     "score=95.5",
			wantKey:   "score",
			wantValue: 95.5,
			wantErr:   false,
		},
		{
			name:      "boolean true",
			input:     "enabled=true",
			wantKey:   "enabled",
			wantValue: true,
			wantErr:   false,
		},
		{
			name:      "boolean false",
			input:     "debug=false",
			wantKey:   "debug",
			wantValue: false,
			wantErr:   false,
		},
		{
			name:      "string with spaces",
			input:     "message=Hello World",
			wantKey:   "message",
			wantValue: "Hello World",
			wantErr:   false,
		},
		{
			name:      "empty value",
			input:     "empty=",
			wantKey:   "empty",
			wantValue: "",
			wantErr:   false,
		},
		{
			name:      "value with equals sign",
			input:     "equation=a=b+c",
			wantKey:   "equation",
			wantValue: "a=b+c",
			wantErr:   false,
		},
		{
			name:      "spaces around key and value",
			input:     " key = value ",
			wantKey:   "key",
			wantValue: "value",
			wantErr:   false,
		},
		{
			name:    "missing equals sign",
			input:   "invalid",
			wantErr: true,
		},
		{
			name:    "empty key",
			input:   "=value",
			wantErr: true,
		},
		{
			name:      "negative integer",
			input:     "temperature=-10",
			wantKey:   "temperature",
			wantValue: -10,
			wantErr:   false,
		},
		{
			name:      "string that looks like number but isn't",
			input:     "id=123abc",
			wantKey:   "id",
			wantValue: "123abc",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := ParseKV(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseKV() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if key != tt.wantKey {
					t.Errorf("ParseKV() key = %v, want %v", key, tt.wantKey)
				}
				if !reflect.DeepEqual(value, tt.wantValue) {
					t.Errorf("ParseKV() value = %v (type: %T), want %v (type: %T)",
						value, value, tt.wantValue, tt.wantValue)
				}
			}
		})
	}
}

func TestParseJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    any
		wantErr bool
	}{
		{
			name:  "simple object",
			input: `{"name": "Alice", "age": 25}`,
			want: map[string]any{
				"name": "Alice",
				"age":  float64(25), // JSON numbers are float64
			},
			wantErr: false,
		},
		{
			name:  "nested object",
			input: `{"user": {"id": 123, "active": true}}`,
			want: map[string]any{
				"user": map[string]any{
					"id":     float64(123),
					"active": true,
				},
			},
			wantErr: false,
		},
		{
			name:    "array",
			input:   `[1, 2, 3]`,
			want:    []any{float64(1), float64(2), float64(3)},
			wantErr: false,
		},
		{
			name:    "string value",
			input:   `"hello"`,
			want:    "hello",
			wantErr: false,
		},
		{
			name:    "number value",
			input:   `42`,
			want:    float64(42),
			wantErr: false,
		},
		{
			name:    "boolean value",
			input:   `true`,
			want:    true,
			wantErr: false,
		},
		{
			name:    "null value",
			input:   `null`,
			want:    nil,
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseFile(t *testing.T) {
	// Create temp directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		fileContent string
		want        any
		wantErr     bool
	}{
		{
			name:        "valid JSON object",
			fileContent: `{"test": "data", "number": 42}`,
			want: map[string]any{
				"test":   "data",
				"number": float64(42),
			},
			wantErr: false,
		},
		{
			name:        "valid JSON array",
			fileContent: `["item1", "item2"]`,
			want:        []any{"item1", "item2"},
			wantErr:     false,
		},
		{
			name:        "invalid JSON",
			fileContent: `{invalid json}`,
			wantErr:     true,
		},
		{
			name:        "empty file",
			fileContent: ``,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			filePath := filepath.Join(tmpDir, tt.name+".json")
			if err := os.WriteFile(filePath, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			got, err := ParseFile(filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseFile() = %v, want %v", got, tt.want)
			}
		})
	}

	// Test non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		_, err := ParseFile("/non/existent/file.json")
		if err == nil {
			t.Errorf("ParseFile() expected error for non-existent file")
		}
	})
}

func TestParseEnv(t *testing.T) {
	// Save current environment and restore after test
	oldEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range oldEnv {
			kv := splitEnv(env)
			os.Setenv(kv[0], kv[1])
		}
	}()

	tests := []struct {
		name    string
		envVars map[string]string
		want    map[string]any
	}{
		{
			name: "GHOST_CONTEXT JSON",
			envVars: map[string]string{
				"GHOST_CONTEXT": `{"user": "alice", "role": "admin"}`,
			},
			want: map[string]any{
				"user": "alice",
				"role": "admin",
			},
		},
		{
			name: "GHOST_CONTEXT_* variables",
			envVars: map[string]string{
				"GHOST_CONTEXT_USER_ID": "123",
				"GHOST_CONTEXT_ENABLED": "true",
				"GHOST_CONTEXT_NAME":    "test",
			},
			want: map[string]any{
				"user_id": 123,
				"enabled": true,
				"name":    "test",
			},
		},
		{
			name: "Mixed GHOST_CONTEXT and GHOST_CONTEXT_*",
			envVars: map[string]string{
				"GHOST_CONTEXT":         `{"base": "value"}`,
				"GHOST_CONTEXT_EXTRA":   "data",
				"GHOST_CONTEXT_COUNTER": "42",
			},
			want: map[string]any{
				"base":    "value",
				"extra":   "data",
				"counter": 42,
			},
		},
		{
			name: "Invalid JSON in GHOST_CONTEXT (ignored)",
			envVars: map[string]string{
				"GHOST_CONTEXT":       `{invalid}`,
				"GHOST_CONTEXT_VALID": "yes",
			},
			want: map[string]any{
				"valid": "yes",
			},
		},
		{
			name: "No context variables",
			envVars: map[string]string{
				"OTHER_VAR": "ignored",
			},
			want: nil,
		},
		{
			name: "Type inference in env vars",
			envVars: map[string]string{
				"GHOST_CONTEXT_STRING": "hello",
				"GHOST_CONTEXT_INT":    "42",
				"GHOST_CONTEXT_FLOAT":  "3.14",
				"GHOST_CONTEXT_BOOL":   "false",
			},
			want: map[string]any{
				"string": "hello",
				"int":    42,
				"float":  3.14,
				"bool":   false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env and set test vars
			os.Clearenv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			got := ParseEnv()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMergeContexts(t *testing.T) {
	tests := []struct {
		name     string
		contexts []any
		want     any
	}{
		{
			name: "merge two maps",
			contexts: []any{
				map[string]any{"a": 1, "b": 2},
				map[string]any{"b": 3, "c": 4},
			},
			want: map[string]any{"a": 1, "b": 3, "c": 4},
		},
		{
			name: "later values override earlier",
			contexts: []any{
				map[string]any{"key": "first"},
				map[string]any{"key": "second"},
				map[string]any{"key": "third"},
			},
			want: map[string]any{"key": "third"},
		},
		{
			name: "nil contexts ignored",
			contexts: []any{
				nil,
				map[string]any{"a": 1},
				nil,
				map[string]any{"b": 2},
			},
			want: map[string]any{"a": 1, "b": 2},
		},
		{
			name:     "all nil",
			contexts: []any{nil, nil, nil},
			want:     nil,
		},
		{
			name:     "empty input",
			contexts: []any{},
			want:     nil,
		},
		{
			name: "non-map value returned as-is",
			contexts: []any{
				"string value",
			},
			want: "string value",
		},
		{
			name: "non-map ignored if maps present",
			contexts: []any{
				map[string]any{"a": 1},
				"ignored",
				map[string]any{"b": 2},
			},
			want: map[string]any{"a": 1, "b": 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MergeContexts(tt.contexts...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MergeContexts() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildContext(t *testing.T) {
	// Save current environment and restore after test
	oldEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range oldEnv {
			kv := splitEnv(env)
			os.Setenv(kv[0], kv[1])
		}
	}()

	// Create temp directory for test files
	tmpDir := t.TempDir()

	// Create a context file
	contextFile := filepath.Join(tmpDir, "context.json")
	os.WriteFile(contextFile, []byte(`{"file": "data", "override": "file"}`), 0644)

	tests := []struct {
		name     string
		jsonStr  string
		kvPairs  []string
		filePath string
		envVars  map[string]string
		want     any
		wantErr  bool
	}{
		{
			name:    "only KV pairs",
			kvPairs: []string{"key1=value1", "key2=42", "key3=true"},
			want: map[string]any{
				"key1": "value1",
				"key2": 42,
				"key3": true,
			},
			wantErr: false,
		},
		{
			name:    "only JSON string",
			jsonStr: `{"json": "data"}`,
			envVars: map[string]string{
				"GHOST_CONTEXT": `{"env": "value", "override": "env"}`,
			},
			want: map[string]any{
				"json":     "data",
				"env":      "value",
				"override": "env",
			},
			wantErr: false,
		},
		{
			name:     "only file",
			filePath: contextFile,
			envVars: map[string]string{
				"GHOST_CONTEXT": `{"env": "value", "override": "env"}`,
			},
			want: map[string]any{
				"file":     "data",
				"env":      "value",
				"override": "file",
			},
			wantErr: false,
		},
		{
			name:     "all sources with precedence",
			jsonStr:  `{"json": "value", "override": "json"}`,
			kvPairs:  []string{"kv=pair", "override=kv"},
			filePath: contextFile,
			envVars: map[string]string{
				"GHOST_CONTEXT": `{"env": "value", "override": "env"}`,
			},
			want: map[string]any{
				"env":      "value",
				"file":     "data",
				"json":     "value",
				"kv":       "pair",
				"override": "kv", // KV has highest precedence
			},
			wantErr: false,
		},
		{
			name:    "invalid KV pair",
			kvPairs: []string{"invalid"},
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			jsonStr: `{invalid}`,
			wantErr: true,
		},
		{
			name:     "non-existent file",
			filePath: "/non/existent/file.json",
			wantErr:  true,
		},
		{
			name:    "empty inputs uses env only",
			jsonStr: "",
			kvPairs: []string{},
			envVars: map[string]string{
				"GHOST_CONTEXT": `{"env": "value", "override": "env"}`,
			},
			want: map[string]any{
				"env":      "value",
				"override": "env",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env and set test vars
			os.Clearenv()
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			got, err := BuildContext(tt.jsonStr, tt.kvPairs, tt.filePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("BuildContext() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Helper function to split environment variable string
func splitEnv(env string) []string {
	parts := []string{"", ""}
	for i := 0; i < len(env); i++ {
		if env[i] == '=' {
			parts[0] = env[:i]
			parts[1] = env[i+1:]
			break
		}
	}
	return parts
}