//go:build windows

package system

import "testing"

func TestTakeSnapshot_NotSimulatedOnWindows(t *testing.T) {
	snap := NewCollector(30).TakeSnapshot()
	if snap.Simulated {
		t.Fatal("Windows snapshots use real collectors and must not be flagged simulated")
	}
}
