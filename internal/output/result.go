package output

type Result struct {
	Command       string  `json:"command"`
	Input         string  `json:"input"`
	Expected      *string `json:"expected,omitempty"`
	Output        string  `json:"output"`
	Stderr        string  `json:"stderr"`
	ExitCode      int     `json:"exit_code"`
	ExecutionTime int64   `json:"execution_time"`
	Score         *int    `json:"score,omitempty"`
}
