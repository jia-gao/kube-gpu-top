package agent

import (
	"context"
	"fmt"
	"testing"

	pb "github.com/jia-gao/kube-gpu-top/api/gpuagent"
	"github.com/jia-gao/kube-gpu-top/pkg/gpu"
	"github.com/jia-gao/kube-gpu-top/pkg/podresources"
)

// --- Mock implementations ---

type mockCollector struct {
	metrics []gpu.DeviceMetrics
	err     error
}

func (m *mockCollector) Collect() ([]gpu.DeviceMetrics, error) {
	return m.metrics, m.err
}

type mockPodMapper struct {
	mapping map[string]podresources.PodGPUMapping
	err     error
}

func (m *mockPodMapper) GetGPUPodMapping(ctx context.Context) (map[string]podresources.PodGPUMapping, error) {
	return m.mapping, m.err
}

// --- Tests ---

func TestGetGPUStatus_TwoGPUs(t *testing.T) {
	collector := &mockCollector{
		metrics: []gpu.DeviceMetrics{
			{
				UUID:           "GPU-aaa",
				Name:           "NVIDIA A100-SXM4-80GB",
				Index:          0,
				GPUUtilization: 85,
				MemUtilization: 60,
				MemUsedBytes:   40 * 1024 * 1024 * 1024,
				MemTotalBytes:  80 * 1024 * 1024 * 1024,
				TemperatureC:   65,
				PowerWatts:     250,
				PowerLimitW:    400,
			},
			{
				UUID:           "GPU-bbb",
				Name:           "NVIDIA A100-SXM4-80GB",
				Index:          1,
				GPUUtilization: 0,
				MemUtilization: 0,
				MemUsedBytes:   0,
				MemTotalBytes:  80 * 1024 * 1024 * 1024,
				TemperatureC:   35,
				PowerWatts:     50,
				PowerLimitW:    400,
			},
		},
	}

	mapper := &mockPodMapper{
		mapping: map[string]podresources.PodGPUMapping{
			"GPU-aaa": {
				Namespace: "ml-team",
				PodName:   "training-job-xyz",
				Container: "train",
			},
		},
	}

	srv := NewServerWithDeps(collector, mapper, "gpu-node-01")
	resp, err := srv.GetGPUStatus(context.Background(), &pb.GPUStatusRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.NodeName != "gpu-node-01" {
		t.Errorf("NodeName = %q, want %q", resp.NodeName, "gpu-node-01")
	}

	if len(resp.Devices) != 2 {
		t.Fatalf("got %d devices, want 2", len(resp.Devices))
	}

	// First GPU: mapped to a pod
	dev0 := resp.Devices[0]
	if dev0.Uuid != "GPU-aaa" {
		t.Errorf("dev0.Uuid = %q, want %q", dev0.Uuid, "GPU-aaa")
	}
	if dev0.GpuUtilization != 85 {
		t.Errorf("dev0.GpuUtilization = %d, want 85", dev0.GpuUtilization)
	}
	if dev0.Pod == nil {
		t.Fatal("dev0.Pod is nil, want pod info")
	}
	if dev0.Pod.Namespace != "ml-team" {
		t.Errorf("dev0.Pod.Namespace = %q, want %q", dev0.Pod.Namespace, "ml-team")
	}
	if dev0.Pod.Name != "training-job-xyz" {
		t.Errorf("dev0.Pod.Name = %q, want %q", dev0.Pod.Name, "training-job-xyz")
	}

	// Second GPU: idle, no pod
	dev1 := resp.Devices[1]
	if dev1.Uuid != "GPU-bbb" {
		t.Errorf("dev1.Uuid = %q, want %q", dev1.Uuid, "GPU-bbb")
	}
	if dev1.Pod != nil {
		t.Errorf("dev1.Pod = %v, want nil", dev1.Pod)
	}
}

func TestGetGPUStatus_NoGPUs(t *testing.T) {
	collector := &mockCollector{metrics: []gpu.DeviceMetrics{}}
	mapper := &mockPodMapper{mapping: map[string]podresources.PodGPUMapping{}}

	srv := NewServerWithDeps(collector, mapper, "node-no-gpu")
	resp, err := srv.GetGPUStatus(context.Background(), &pb.GPUStatusRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Devices) != 0 {
		t.Errorf("got %d devices, want 0", len(resp.Devices))
	}
}

func TestGetGPUStatus_CollectorError(t *testing.T) {
	collector := &mockCollector{err: fmt.Errorf("NVML init failed")}
	mapper := &mockPodMapper{mapping: map[string]podresources.PodGPUMapping{}}

	srv := NewServerWithDeps(collector, mapper, "node-1")
	_, err := srv.GetGPUStatus(context.Background(), &pb.GPUStatusRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "collecting GPU metrics: NVML init failed" {
		t.Errorf("error = %q, want %q", got, "collecting GPU metrics: NVML init failed")
	}
}

func TestGetGPUStatus_PodMappingError(t *testing.T) {
	collector := &mockCollector{
		metrics: []gpu.DeviceMetrics{
			{UUID: "GPU-ccc", Name: "T4", Index: 0},
		},
	}
	mapper := &mockPodMapper{err: fmt.Errorf("kubelet socket not found")}

	srv := NewServerWithDeps(collector, mapper, "node-1")
	_, err := srv.GetGPUStatus(context.Background(), &pb.GPUStatusRequest{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got := err.Error(); got != "getting pod mapping: kubelet socket not found" {
		t.Errorf("error = %q, want %q", got, "getting pod mapping: kubelet socket not found")
	}
}
