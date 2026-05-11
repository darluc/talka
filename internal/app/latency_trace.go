package app

import (
	"sync"
	"time"
)

const defaultLatencyTraceLimit = 20

type LatencyTrace struct {
	TraceID             string    `json:"trace_id"`
	DeviceID            string    `json:"device_id"`
	AcceptedAt          time.Time `json:"accepted_at"`
	AudioStopReceivedAt time.Time `json:"audio_stop_received_at,omitzero"`
	CompletedAt         time.Time `json:"completed_at,omitzero"`
	InsertCompletedAt   time.Time `json:"insert_completed_at,omitzero"`
	BufferedMessages    int       `json:"buffered_messages"`
	Frames              int       `json:"frames"`
	AudioMSEstimate     int64     `json:"audio_ms_estimate"`
	DecryptDecodeMS     int64     `json:"decrypt_decode_ms"`
	ASRMS               int64     `json:"asr_ms"`
	LLMMS               int64     `json:"llm_ms"`
	ResponseWriteMS     int64     `json:"response_write_ms"`
	InsertMS            int64     `json:"insert_ms"`
	TotalAfterStopMS    int64     `json:"total_after_stop_ms"`
	RawTranscriptChars  int       `json:"raw_transcript_chars"`
	FinalTextChars      int       `json:"final_text_chars"`
	ErrorStage          string    `json:"error_stage,omitempty"`
	Error               string    `json:"error,omitempty"`
}

type LatencyRecorder struct {
	mu     sync.RWMutex
	limit  int
	traces []LatencyTrace
	index  map[string]int
}

func NewLatencyRecorder(limit int) *LatencyRecorder {
	if limit <= 0 {
		limit = defaultLatencyTraceLimit
	}
	return &LatencyRecorder{limit: limit, index: map[string]int{}}
}

func (r *LatencyRecorder) Start(traceID, deviceID string, acceptedAt time.Time) {
	if r == nil || traceID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.index[traceID]; ok {
		r.traces[existing].DeviceID = deviceID
		r.traces[existing].AcceptedAt = acceptedAt
		return
	}

	r.traces = append([]LatencyTrace{{TraceID: traceID, DeviceID: deviceID, AcceptedAt: acceptedAt}}, r.traces...)
	if len(r.traces) > r.limit {
		r.traces = r.traces[:r.limit]
	}
	r.reindexLocked()
}

func (r *LatencyRecorder) Update(traceID string, update func(*LatencyTrace)) {
	if r == nil || traceID == "" || update == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	pos, ok := r.index[traceID]
	if !ok {
		return
	}
	update(&r.traces[pos])
}

func (r *LatencyRecorder) Snapshot() []LatencyTrace {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	copyTraces := make([]LatencyTrace, len(r.traces))
	copy(copyTraces, r.traces)
	return copyTraces
}

func (r *LatencyRecorder) reindexLocked() {
	r.index = map[string]int{}
	for i, trace := range r.traces {
		r.index[trace.TraceID] = i
	}
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}
