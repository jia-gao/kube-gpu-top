package gpu

import "testing"

// TestCollectorImplementsInterface verifies that *Collector satisfies MetricsCollector.
func TestCollectorImplementsInterface(t *testing.T) {
	var _ MetricsCollector = (*Collector)(nil)
}

// Note: Collect() cannot be tested without a real NVIDIA GPU and NVML driver.
// On a GPU node, you would call Init(), defer Shutdown(), and then test Collect().
