package metrics

import (
	"testing"
)

func TestRecordTxArrivalAfterCommit_BucketPlacement(t *testing.T) {
	m := &Metrics{}

	// Record values landing in different buckets:
	// Boundaries: 5, 10, 20, 50, 100, 200, 500, 1000
	m.RecordTxArrivalAfterCommit(3)    // bucket 0 (le 5ms)
	m.RecordTxArrivalAfterCommit(5)    // bucket 0 (le 5ms, boundary inclusive)
	m.RecordTxArrivalAfterCommit(7)    // bucket 1 (le 10ms)
	m.RecordTxArrivalAfterCommit(15)   // bucket 2 (le 20ms)
	m.RecordTxArrivalAfterCommit(80)   // bucket 4 (le 100ms)
	m.RecordTxArrivalAfterCommit(1500) // bucket 7 (overflow, >1000ms)

	if m.TxArrivalAfterCommitCount.Load() != 6 {
		t.Errorf("expected count=6, got %d", m.TxArrivalAfterCommitCount.Load())
	}

	// Check individual (non-cumulative) bucket counts
	expected := [8]uint64{2, 1, 1, 0, 1, 0, 0, 1}
	for i, want := range expected {
		got := m.TxArrivalAfterCommitBuckets[i].Load()
		if got != want {
			t.Errorf("bucket[%d] (le %.0fms): expected %d, got %d",
				i, TxArrivalAfterCommitBoundariesMs[i], want, got)
		}
	}

	// Check sum: (3+5+7+15+80+1500) * 1000 microseconds = 1_610_000
	expectedSum := uint64((3 + 5 + 7 + 15 + 80 + 1500) * 1000)
	if m.TxArrivalAfterCommitSum.Load() != expectedSum {
		t.Errorf("expected sum=%d, got %d", expectedSum, m.TxArrivalAfterCommitSum.Load())
	}
}

func TestGetTxArrivalAfterCommitCumulativeBuckets(t *testing.T) {
	m := &Metrics{}

	m.RecordTxArrivalAfterCommit(3)  // bucket 0
	m.RecordTxArrivalAfterCommit(7)  // bucket 1
	m.RecordTxArrivalAfterCommit(15) // bucket 2
	m.RecordTxArrivalAfterCommit(25) // bucket 3

	buckets, count, sumMs := m.GetTxArrivalAfterCommitCumulativeBuckets()

	if count != 4 {
		t.Errorf("expected count=4, got %d", count)
	}

	expectedSum := (3.0 + 7.0 + 15.0 + 25.0)
	if sumMs < expectedSum-0.01 || sumMs > expectedSum+0.01 {
		t.Errorf("expected sumMs=%.1f, got %.1f", expectedSum, sumMs)
	}

	// Cumulative: le5=1, le10=2, le20=3, le50=4, le100=4, le200=4, le500=4, le1000=4
	expectedCumulative := map[float64]uint64{
		5:    1,
		10:   2,
		20:   3,
		50:   4,
		100:  4,
		200:  4,
		500:  4,
		1000: 4,
	}
	for boundary, want := range expectedCumulative {
		got := buckets[boundary]
		if got != want {
			t.Errorf("cumulative bucket le_%.0f: expected %d, got %d", boundary, want, got)
		}
	}
}

func TestRecordTxArrivalAfterCommit_ZeroObservations(t *testing.T) {
	m := &Metrics{}

	buckets, count, sumMs := m.GetTxArrivalAfterCommitCumulativeBuckets()

	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
	if sumMs != 0 {
		t.Errorf("expected sumMs=0, got %f", sumMs)
	}
	for boundary, v := range buckets {
		if v != 0 {
			t.Errorf("expected bucket le_%.0f=0, got %d", boundary, v)
		}
	}
}

func TestGetAvgTxArrivalAfterCommitMs(t *testing.T) {
	m := &Metrics{}

	// Zero observations should return 0, not NaN
	avg := m.getAvgTxArrivalAfterCommitMs()
	if avg != 0 {
		t.Errorf("expected avg=0 with no observations, got %f", avg)
	}

	m.RecordTxArrivalAfterCommit(10)
	m.RecordTxArrivalAfterCommit(20)
	m.RecordTxArrivalAfterCommit(30)

	avg = m.getAvgTxArrivalAfterCommitMs()
	expected := 20.0 // (10+20+30)/3
	if avg < expected-0.01 || avg > expected+0.01 {
		t.Errorf("expected avg=%.1f, got %.1f", expected, avg)
	}
}

func TestRecordTxArrivalAfterCommit_BoundaryValues(t *testing.T) {
	m := &Metrics{}

	// Test exact boundary values go into the correct bucket (inclusive)
	for _, boundary := range TxArrivalAfterCommitBoundariesMs {
		m.RecordTxArrivalAfterCommit(boundary)
	}

	// Each boundary value should land in its own bucket
	for i := range TxArrivalAfterCommitBoundariesMs {
		got := m.TxArrivalAfterCommitBuckets[i].Load()
		if got != 1 {
			t.Errorf("bucket[%d] (le %.0fms): expected 1, got %d",
				i, TxArrivalAfterCommitBoundariesMs[i], got)
		}
	}
}
