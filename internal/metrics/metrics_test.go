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

// --- Priority round-trip histogram tests ---

func TestRecordPriorityRoundTrip_BucketPlacement(t *testing.T) {
	m := &Metrics{}

	// Boundaries: 2, 4, 6, 8, 10, 15, 20, 30, 50, 100
	m.RecordPriorityRoundTrip(1)   // bucket 0 (le 2ms)
	m.RecordPriorityRoundTrip(2)   // bucket 0 (le 2ms, boundary inclusive)
	m.RecordPriorityRoundTrip(5)   // bucket 2 (le 6ms)
	m.RecordPriorityRoundTrip(9)   // bucket 4 (le 10ms)
	m.RecordPriorityRoundTrip(12)  // bucket 5 (le 15ms)
	m.RecordPriorityRoundTrip(25)  // bucket 7 (le 30ms)
	m.RecordPriorityRoundTrip(200) // bucket 9 (overflow, >100ms)

	if m.PriorityRoundTripCount.Load() != 7 {
		t.Errorf("expected count=7, got %d", m.PriorityRoundTripCount.Load())
	}

	// Non-cumulative buckets
	expected := [10]uint64{2, 0, 1, 0, 1, 1, 0, 1, 0, 1}
	for i, want := range expected {
		got := m.PriorityRoundTripBuckets[i].Load()
		if got != want {
			t.Errorf("bucket[%d] (le %.0fms): expected %d, got %d",
				i, PriorityRoundTripBoundariesMs[i], want, got)
		}
	}

	// Sum: (1+2+5+9+12+25+200) * 1000 = 254_000 microseconds
	expectedSum := uint64((1 + 2 + 5 + 9 + 12 + 25 + 200) * 1000)
	if m.PriorityRoundTripSum.Load() != expectedSum {
		t.Errorf("expected sum=%d, got %d", expectedSum, m.PriorityRoundTripSum.Load())
	}
}

func TestGetPriorityRoundTripCumulativeBuckets(t *testing.T) {
	m := &Metrics{}

	m.RecordPriorityRoundTrip(3)  // bucket 1 (le 4ms)
	m.RecordPriorityRoundTrip(7)  // bucket 3 (le 8ms)
	m.RecordPriorityRoundTrip(14) // bucket 5 (le 15ms)

	buckets, count, sumMs := m.GetPriorityRoundTripCumulativeBuckets()

	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}

	expectedSum := 3.0 + 7.0 + 14.0
	if sumMs < expectedSum-0.01 || sumMs > expectedSum+0.01 {
		t.Errorf("expected sumMs=%.1f, got %.1f", expectedSum, sumMs)
	}

	// Cumulative: le2=0, le4=1, le6=1, le8=2, le10=2, le15=3, le20=3, le30=3, le50=3, le100=3
	expectedCumulative := map[float64]uint64{
		2: 0, 4: 1, 6: 1, 8: 2, 10: 2, 15: 3, 20: 3, 30: 3, 50: 3, 100: 3,
	}
	for boundary, want := range expectedCumulative {
		got := buckets[boundary]
		if got != want {
			t.Errorf("cumulative bucket le_%.0f: expected %d, got %d", boundary, want, got)
		}
	}
}

func TestGetAvgPriorityRoundTripMs(t *testing.T) {
	m := &Metrics{}

	// Zero observations should return 0
	avg := m.getAvgPriorityRoundTripMs()
	if avg != 0 {
		t.Errorf("expected avg=0 with no observations, got %f", avg)
	}

	m.RecordPriorityRoundTrip(8)
	m.RecordPriorityRoundTrip(10)
	m.RecordPriorityRoundTrip(12)

	avg = m.getAvgPriorityRoundTripMs()
	expected := 10.0 // (8+10+12)/3
	if avg < expected-0.01 || avg > expected+0.01 {
		t.Errorf("expected avg=%.1f, got %.1f", expected, avg)
	}
}

func TestRecordPriorityRoundTrip_ZeroObservations(t *testing.T) {
	m := &Metrics{}

	buckets, count, sumMs := m.GetPriorityRoundTripCumulativeBuckets()

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
