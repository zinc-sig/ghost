package output

type Result struct {
	Input         *string `json:"input,omitempty"`
	Output        *string `json:"output,omitempty"`
	Stderr        *string `json:"stderr,omitempty"`
	ExitCode      int     `json:"exit_code"`
	ExecutionTime int64   `json:"execution_time"`
	Score         *int    `json:"score,omitempty"`
}