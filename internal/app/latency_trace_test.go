package app

import (
	"testing"
	"time"
)

func TestLatencyRecorderKeepsMostRecentTracesNewestFirst(t *testing.T) {
	recorder := NewLatencyRecorder(2)
	base := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)

	recorder.Start("trace-1", "device-a", base)
	recorder.Start("trace-2", "device-a", base.Add(time.Second))
	recorder.Start("trace-3", "device-a", base.Add(2*time.Second))

	traces := recorder.Snapshot()
	if len(traces) != 2 {
		t.Fatalf("len(Snapshot()) = %d, want 2", len(traces))
	}
	if got, want := traces[0].TraceID, "trace-3"; got != want {
		t.Fatalf("latest TraceID = %q, want %q", got, want)
	}
	if got, want := traces[1].TraceID, "trace-2"; got != want {
		t.Fatalf("second TraceID = %q, want %q", got, want)
	}
}

func TestLatencyRecorderUpdateStoresDurationsAndProtectsSnapshot(t *testing.T) {
	recorder := NewLatencyRecorder(4)
	acceptedAt := time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC)
	recorder.Start("trace-1", "device-a", acceptedAt)

	recorder.Update("trace-1", func(trace *LatencyTrace) {
		trace.BufferedMessages = 42
		trace.Frames = 40
		trace.DecryptDecodeMS = 12
		trace.ASRMS = 345
		trace.LLMMS = 678
		trace.TotalAfterStopMS = 1035
	})

	first := recorder.Snapshot()
	if got, want := first[0].BufferedMessages, 42; got != want {
		t.Fatalf("BufferedMessages = %d, want %d", got, want)
	}
	if got, want := first[0].ASRMS, int64(345); got != want {
		t.Fatalf("ASRMS = %d, want %d", got, want)
	}

	first[0].ASRMS = 1
	second := recorder.Snapshot()
	if got, want := second[0].ASRMS, int64(345); got != want {
		t.Fatalf("snapshot mutation leaked ASRMS = %d, want %d", got, want)
	}
}
