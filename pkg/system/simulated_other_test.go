//go:build !windows

package system

import "testing"

func TestTakeSnapshot_SimulatedOffWindows(t *testing.T) {
	snap := NewCollector(30).TakeSnapshot()
	if !snap.Simulated {
		t.Fatal("non-Windows snapshots use placeholder values and must be flagged simulated")
	}
}
