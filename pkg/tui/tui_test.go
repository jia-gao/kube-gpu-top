package tui

import (
	"strings"
	"testing"
	"time"

	pb "github.com/jia-gao/kube-gpu-top/api/gpuagent"
)

func sampleResponses() []*pb.GPUStatusResponse {
	return []*pb.GPUStatusResponse{
		{
			NodeName: "node-1",
			Devices: []*pb.GPUDevice{
				{
					Uuid:           "gpu-0001",
					Name:           "NVIDIA A100-SXM4-80GB",
					Index:          0,
					GpuUtilization: 72,
					MemUsedBytes:   40 * 1024 * 1024 * 1024,
					MemTotalBytes:  80 * 1024 * 1024 * 1024,
					TemperatureC:   65,
					PowerWatts:     280,
					Pod: &pb.PodInfo{
						Namespace: "ml-training",
						Name:      "train-llama-0",
					},
				},
				{
					Uuid:           "gpu-0002",
					Name:           "NVIDIA A100-SXM4-80GB",
					Index:          1,
					GpuUtilization: 5,
					MemUsedBytes:   2 * 1024 * 1024 * 1024,
					MemTotalBytes:  80 * 1024 * 1024 * 1024,
					TemperatureC:   38,
					PowerWatts:     60,
					Pod: &pb.PodInfo{
						Namespace: "default",
						Name:      "idle-notebook",
					},
				},
			},
		},
		{
			NodeName: "node-2",
			Devices: []*pb.GPUDevice{
				{
					Uuid:           "gpu-0003",
					Name:           "NVIDIA H100-SXM5-80GB",
					Index:          0,
					GpuUtilization: 95,
					MemUsedBytes:   70 * 1024 * 1024 * 1024,
					MemTotalBytes:  80 * 1024 * 1024 * 1024,
					TemperatureC:   78,
					PowerWatts:     650,
					Pod: &pb.PodInfo{
						Namespace: "ml-training",
						Name:      "train-gpt-0",
					},
				},
			},
		},
	}
}

func stubQuery() ([]*pb.GPUStatusResponse, error) {
	return sampleResponses(), nil
}

func TestNewModel(t *testing.T) {
	m := New(stubQuery, 2*time.Second)
	if m.Interval != 2*time.Second {
		t.Errorf("Interval = %v, want 2s", m.Interval)
	}
	if m.sortCol != SortUtil {
		t.Errorf("sortCol = %v, want SortUtil", m.sortCol)
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	if m.inputMode != ModeNormal {
		t.Errorf("inputMode = %v, want ModeNormal", m.inputMode)
	}
}

func TestViewRendersWithoutPanic(t *testing.T) {
	m := New(stubQuery, 2*time.Second)
	// Simulate data arrival.
	rows, gpuCount, nodeCount := flattenResponses(sampleResponses())
	m.rows = rows
	m.gpuCount = gpuCount
	m.nodeCount = nodeCount
	m.lastFetch = time.Now()

	view := m.View()

	if view == "" {
		t.Fatal("View() returned empty string")
	}
	if !strings.Contains(view, "kube-gpu-top") {
		t.Error("View() missing header")
	}
	if !strings.Contains(view, "NODE") {
		t.Error("View() missing column header")
	}
	if !strings.Contains(view, "node-1") {
		t.Error("View() missing node-1 data")
	}
	if !strings.Contains(view, "node-2") {
		t.Error("View() missing node-2 data")
	}
}

func TestSortCycling(t *testing.T) {
	tests := []struct {
		start SortColumn
		want  SortColumn
	}{
		{SortUtil, SortMem},
		{SortMem, SortPower},
		{SortPower, SortNode},
		{SortNode, SortNamespace},
		{SortNamespace, SortUtil},
	}
	for _, tt := range tests {
		got := tt.start.Next()
		if got != tt.want {
			t.Errorf("%v.Next() = %v, want %v", tt.start, got, tt.want)
		}
	}
}

func TestSortColumnString(t *testing.T) {
	tests := []struct {
		col  SortColumn
		want string
	}{
		{SortUtil, "UTIL"},
		{SortMem, "MEM"},
		{SortPower, "POWER"},
		{SortNode, "NODE"},
		{SortNamespace, "NAMESPACE"},
	}
	for _, tt := range tests {
		if got := tt.col.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.col, got, tt.want)
		}
	}
}

func TestFilteredRowsByNamespace(t *testing.T) {
	m := New(stubQuery, 2*time.Second)
	rows, _, _ := flattenResponses(sampleResponses())
	m.rows = rows
	m.namespaceFilter = "ml-training"

	filtered := m.filteredRows()
	if len(filtered) != 2 {
		t.Fatalf("expected 2 rows for ml-training, got %d", len(filtered))
	}
	for _, r := range filtered {
		if r.Namespace != "ml-training" {
			t.Errorf("unexpected namespace %q in filtered results", r.Namespace)
		}
	}
}

func TestFilteredRowsBySearch(t *testing.T) {
	m := New(stubQuery, 2*time.Second)
	rows, _, _ := flattenResponses(sampleResponses())
	m.rows = rows
	m.searchFilter = "llama"

	filtered := m.filteredRows()
	if len(filtered) != 1 {
		t.Fatalf("expected 1 row for 'llama' search, got %d", len(filtered))
	}
	if filtered[0].Pod != "train-llama-0" {
		t.Errorf("expected pod train-llama-0, got %s", filtered[0].Pod)
	}
}

func TestFlattenResponses(t *testing.T) {
	rows, gpuCount, nodeCount := flattenResponses(sampleResponses())
	if gpuCount != 3 {
		t.Errorf("gpuCount = %d, want 3", gpuCount)
	}
	if nodeCount != 2 {
		t.Errorf("nodeCount = %d, want 2", nodeCount)
	}
	if len(rows) != 3 {
		t.Errorf("len(rows) = %d, want 3", len(rows))
	}
}

func TestUtilBarRendering(t *testing.T) {
	// Just ensure it does not panic and contains the percentage.
	bar := renderUtilBar(42)
	if !strings.Contains(bar, "42%") {
		t.Errorf("renderUtilBar(42) = %q, missing 42%%", bar)
	}

	bar0 := renderUtilBar(0)
	if !strings.Contains(bar0, "0%") {
		t.Errorf("renderUtilBar(0) = %q, missing 0%%", bar0)
	}

	bar100 := renderUtilBar(100)
	if !strings.Contains(bar100, "100%") {
		t.Errorf("renderUtilBar(100) = %q, missing 100%%", bar100)
	}
}

func TestSortRows(t *testing.T) {
	rows := []Row{
		{Node: "b", Util: 10, MemUsed: 100, Power: 50},
		{Node: "a", Util: 90, MemUsed: 500, Power: 200},
		{Node: "c", Util: 50, MemUsed: 300, Power: 100},
	}

	sortRows(rows, SortUtil)
	if rows[0].Util != 90 || rows[1].Util != 50 || rows[2].Util != 10 {
		t.Errorf("SortUtil: expected [90,50,10], got [%d,%d,%d]", rows[0].Util, rows[1].Util, rows[2].Util)
	}

	sortRows(rows, SortNode)
	if rows[0].Node != "a" || rows[1].Node != "b" || rows[2].Node != "c" {
		t.Errorf("SortNode: expected [a,b,c], got [%s,%s,%s]", rows[0].Node, rows[1].Node, rows[2].Node)
	}
}
