package system

import (
	"testing"
)

func TestCollector_TakeSnapshot(t *testing.T) {
	c := NewCollector(30)
	snap := c.TakeSnapshot()

	if snap.Memory.TotalBytes == 0 {
		t.Errorf("expected TotalBytes > 0, got 0")
	}

	if snap.CPUPercent < 0 || snap.CPUPercent > 100 {
		t.Errorf("expected CPUPercent between 0 and 100, got %f", snap.CPUPercent)
	}

	if len(snap.Disks) == 0 {
		t.Logf("warning: no fixed disks reported")
	} else {
		d := snap.Disks[0]
		if d.TotalBytes == 0 {
			t.Errorf("expected disk TotalBytes > 0, got 0")
		}
	}

	if snap.ProcessCount <= 0 {
		t.Errorf("expected ProcessCount > 0, got %d", snap.ProcessCount)
	}
}

func TestFormatSparkline(t *testing.T) {
	values := []float64{0, 25, 50, 75, 100}
	spark := FormatSparkline(values, 10)
	if len([]rune(spark)) != 5 {
		t.Errorf("expected length 5 runes, got %d (%q)", len([]rune(spark)), spark)
	}

	empty := FormatSparkline(nil, 10)
	if empty != "" {
		t.Errorf("expected empty string for nil slice, got %q", empty)
	}
}

func TestHumanSize(t *testing.T) {
	tests := []struct {
		bytes uint64
		want  string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024 * 5, "5.0 MB"},
		{1024 * 1024 * 1024 * 16, "16.0 GB"},
	}

	for _, tc := range tests {
		got := HumanSize(tc.bytes)
		if got != tc.want {
			t.Errorf("HumanSize(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}
