package waste

import (
	"testing"
)

func TestCostTableLookup(t *testing.T) {
	tests := []struct {
		name string
		gpu  string
		want float64
	}{
		{"a100 sxm4", "NVIDIA A100-SXM4-80GB", 2.50},
		{"a100 pcie", "A100-PCIE-40GB", 2.50},
		{"h100", "NVIDIA H100-SXM5-80GB", 5.00},
		{"l4", "NVIDIA L4", 0.70},
		{"t4", "NVIDIA T4", 0.35},
		{"unknown", "NVIDIA Fake-GPU-9000", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultCostTable.Lookup(tt.gpu)
			if got != tt.want {
				t.Errorf("Lookup(%q) = %v, want %v", tt.gpu, got, tt.want)
			}
		})
	}
}

func TestCostTableLookupPrefersLongestMatch(t *testing.T) {
	ct := CostTable{
		"A100":      2.50,
		"A100-80GB": 3.00,
	}
	got := ct.Lookup("NVIDIA A100-80GB")
	if got != 3.00 {
		t.Errorf("expected longest-match 3.00, got %v", got)
	}
}

func TestAnalyzeFlagsIdleGPU(t *testing.T) {
	// One GPU, 3 samples, all effectively idle.
	samples := []Sample{
		{NodeName: "n1", GPUUUID: "gpu-1", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 2, MemUsedBytes: 1 << 30, MemTotalBytes: 80 << 30,
			PodNamespace: "dev", PodName: "notebook-alice"},
		{NodeName: "n1", GPUUUID: "gpu-1", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 0, MemUsedBytes: 1 << 30, MemTotalBytes: 80 << 30,
			PodNamespace: "dev", PodName: "notebook-alice"},
		{NodeName: "n1", GPUUUID: "gpu-1", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 4, MemUsedBytes: 1 << 30, MemTotalBytes: 80 << 30,
			PodNamespace: "dev", PodName: "notebook-alice"},
	}

	findings := Analyze(samples, DefaultThresholds, DefaultCostTable)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Reason != ReasonIdle {
		t.Errorf("expected ReasonIdle, got %q", f.Reason)
	}
	if f.HourlyUSD != 2.50 {
		t.Errorf("expected $2.50/hr, got %v", f.HourlyUSD)
	}
	if f.AvgUtil != 2 {
		t.Errorf("expected avg util 2, got %v", f.AvgUtil)
	}
}

func TestAnalyzeFlagsComputeIdleWithLoadedMemory(t *testing.T) {
	// Memory is held (a loaded model), but compute is idle.
	samples := []Sample{
		{NodeName: "n1", GPUUUID: "gpu-2", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 1, MemUsedBytes: 70 << 30, MemTotalBytes: 80 << 30,
			PodNamespace: "inference", PodName: "vllm-idle"},
		{NodeName: "n1", GPUUUID: "gpu-2", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 3, MemUsedBytes: 70 << 30, MemTotalBytes: 80 << 30,
			PodNamespace: "inference", PodName: "vllm-idle"},
	}
	findings := Analyze(samples, DefaultThresholds, DefaultCostTable)
	if len(findings) != 1 || findings[0].Reason != ReasonComputeIdle {
		t.Fatalf("expected 1 compute-idle finding, got %+v", findings)
	}
}

func TestAnalyzeSkipsBusyGPU(t *testing.T) {
	samples := []Sample{
		{NodeName: "n1", GPUUUID: "gpu-3", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 95, MemUsedBytes: 72 << 30, MemTotalBytes: 80 << 30,
			PodNamespace: "ml", PodName: "train"},
	}
	findings := Analyze(samples, DefaultThresholds, DefaultCostTable)
	if len(findings) != 0 {
		t.Errorf("expected no findings for busy GPU, got %d", len(findings))
	}
}

func TestAnalyzeSkipsUnclaimedGPU(t *testing.T) {
	// No pod attribution — not counted as pod waste.
	samples := []Sample{
		{NodeName: "n1", GPUUUID: "gpu-4", GPUName: "NVIDIA A100-SXM4-80GB",
			GPUUtilization: 0, MemUsedBytes: 0, MemTotalBytes: 80 << 30},
	}
	findings := Analyze(samples, DefaultThresholds, DefaultCostTable)
	if len(findings) != 0 {
		t.Errorf("expected no findings for unclaimed GPU, got %d", len(findings))
	}
}

func TestAnalyzeSortsByMonthlyCostDesc(t *testing.T) {
	samples := []Sample{
		// Cheap idle (T4)
		{NodeName: "n1", GPUUUID: "g-t4", GPUName: "NVIDIA T4",
			GPUUtilization: 0, MemUsedBytes: 0, MemTotalBytes: 16 << 30,
			PodNamespace: "x", PodName: "p1"},
		// Expensive idle (H100)
		{NodeName: "n2", GPUUUID: "g-h100", GPUName: "NVIDIA H100-SXM5-80GB",
			GPUUtilization: 1, MemUsedBytes: 0, MemTotalBytes: 80 << 30,
			PodNamespace: "x", PodName: "p2"},
	}
	findings := Analyze(samples, DefaultThresholds, DefaultCostTable)
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(findings))
	}
	if findings[0].GPUUUID != "g-h100" {
		t.Errorf("expected H100 first (most expensive), got %q", findings[0].GPUUUID)
	}
}
