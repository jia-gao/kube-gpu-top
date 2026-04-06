// Package agent implements the gRPC server for the kube-gpu-agent DaemonSet.
package agent

import (
	"context"
	"fmt"
	"os"

	pb "github.com/jia-gao/kube-gpu-top/api/gpuagent"
	"github.com/jia-gao/kube-gpu-top/pkg/gpu"
	"github.com/jia-gao/kube-gpu-top/pkg/podresources"
)

// Server implements the GPUAgentService gRPC server.
type Server struct {
	pb.UnimplementedGPUAgentServiceServer
	gpuCollector gpu.MetricsCollector
	podClient    podresources.PodMapper
	nodeName     string
}

// NewServer creates a new agent gRPC server with default NVML and kubelet backends.
func NewServer(podResourcesSocket string) *Server {
	nodeName, _ := os.Hostname()
	if n := os.Getenv("NODE_NAME"); n != "" {
		nodeName = n
	}
	return &Server{
		gpuCollector: gpu.NewCollector(),
		podClient:    podresources.NewClient(podResourcesSocket),
		nodeName:     nodeName,
	}
}

// NewServerWithDeps creates a server with injected dependencies (for testing).
func NewServerWithDeps(collector gpu.MetricsCollector, mapper podresources.PodMapper, nodeName string) *Server {
	return &Server{
		gpuCollector: collector,
		podClient:    mapper,
		nodeName:     nodeName,
	}
}

// GetGPUStatus collects GPU metrics and joins with pod attribution data.
func (s *Server) GetGPUStatus(ctx context.Context, _ *pb.GPUStatusRequest) (*pb.GPUStatusResponse, error) {
	metrics, err := s.gpuCollector.Collect()
	if err != nil {
		return nil, fmt.Errorf("collecting GPU metrics: %w", err)
	}

	podMapping, err := s.podClient.GetGPUPodMapping(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting pod mapping: %w", err)
	}

	resp := &pb.GPUStatusResponse{
		NodeName: s.nodeName,
		Devices:  make([]*pb.GPUDevice, 0, len(metrics)),
	}

	for _, m := range metrics {
		dev := &pb.GPUDevice{
			Uuid:           m.UUID,
			Name:           m.Name,
			Index:          uint32(m.Index),
			GpuUtilization: m.GPUUtilization,
			MemUtilization: m.MemUtilization,
			MemUsedBytes:   m.MemUsedBytes,
			MemTotalBytes:  m.MemTotalBytes,
			TemperatureC:   m.TemperatureC,
			PowerWatts:     m.PowerWatts,
			PowerLimitWatts: m.PowerLimitW,
		}

		// Join GPU UUID with pod mapping
		if pod, ok := podMapping[m.UUID]; ok {
			dev.Pod = &pb.PodInfo{
				Namespace: pod.Namespace,
				Name:      pod.PodName,
				Container: pod.Container,
			}
		}

		resp.Devices = append(resp.Devices, dev)
	}

	return resp, nil
}
