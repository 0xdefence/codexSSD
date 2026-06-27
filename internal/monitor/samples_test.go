package monitor

import (
	"testing"
	"time"
)

func TestAppendSampleCapsLength(t *testing.T) {
	var h []Sample
	base := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		h = AppendSample(h, Sample{At: base.Add(time.Duration(i) * time.Minute), TotalBytes: int64(i)}, 3)
	}
	if len(h) != 3 {
		t.Fatalf("len = %d, want 3 (capped)", len(h))
	}
	// Oldest entries evicted; newest kept in order.
	if h[0].TotalBytes != 2 || h[2].TotalBytes != 4 {
		t.Errorf("window = %d..%d, want 2..4", h[0].TotalBytes, h[2].TotalBytes)
	}
}

func TestAppendSampleBelowCap(t *testing.T) {
	var h []Sample
	h = AppendSample(h, Sample{TotalBytes: 1}, 10)
	h = AppendSample(h, Sample{TotalBytes: 2}, 10)
	if len(h) != 2 {
		t.Fatalf("len = %d, want 2", len(h))
	}
}
