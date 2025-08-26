package output

type Result struct {
	Input         string `json:"input"`
	Output        string `json:"output"`
	Stderr        string `json:"stderr"`
	ExitCode      int    `json:"exit_code"`
	ExecutionTime int64  `json:"execution_time"`
	Score         *int   `json:"score,omitempty"`
}
