package output

import (
	"bytes"
	"encoding/json"
	"errors"
)

// TrailerSchema is the current result-trailer schema version. It is the highest
// schema this ghost emits. Frozen at 1 by the sandbox-runtime contract; bump
// only with core.
const TrailerSchema = 1

// Trailer is the structured result a supervised execution emits when its child
// finishes. The JSON shape is frozen by the sandbox-runtime contract
// (backend.ResultTrailer): field names and types must not change without a
// coordinated schema bump on both ghost and core.
type Trailer struct {
	Schema      int   `json:"schema"`
	ExitCode    int   `json:"exit_code"`
	PeakMemoryB int64 `json:"peak_memory_bytes"`
	OOMKilled   bool  `json:"oom_killed"`
	Truncated   bool  `json:"truncated"`
	DurationMs  int64 `json:"duration_ms"`
}

// Result-stream frame sentinels. The frame lets a cluster backend (Kubernetes,
// Nomad) recover the trailer from the exec stream, where it is interleaved with
// the child's real output. Two RS (0x1e) bytes plus the literal token guard
// against false matches: RS bytes do not occur in normal text or JSON.
const (
	frameStart = "\x1e\x1eZINC-RESULT\x1e"
	frameEnd   = "\x1e\x1e"
)

// EncodeFrame renders the trailer as a single-line stream frame:
//
//	\x1e\x1eZINC-RESULT\x1e<compact-json>\x1e\x1e\n
//
// The JSON is compact (no indentation); the trailer has no string fields, so it
// can never contain a newline and the frame is always one line.
func EncodeFrame(t Trailer) ([]byte, error) {
	data, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(frameStart)
	buf.Write(data)
	buf.WriteString(frameEnd)
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// ErrNoFrame is returned by DecodeFrame when no result frame is present.
var ErrNoFrame = errors.New("output: no result frame found")

// DecodeFrame extracts and parses the last result frame from data. It scans for
// the sentinel pair so a lone RS byte in surrounding output cannot false-match.
func DecodeFrame(data []byte) (Trailer, error) {
	var t Trailer
	start := bytes.LastIndex(data, []byte(frameStart))
	if start < 0 {
		return t, ErrNoFrame
	}
	jsonStart := start + len(frameStart)
	end := bytes.Index(data[jsonStart:], []byte(frameEnd))
	if end < 0 {
		return t, ErrNoFrame
	}
	if err := json.Unmarshal(data[jsonStart:jsonStart+end], &t); err != nil {
		return t, err
	}
	return t, nil
}
