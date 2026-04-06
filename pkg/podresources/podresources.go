// Package podresources provides GPU-to-pod mapping via the kubelet Pod Resources API.
package podresources

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	podresourcesv1 "k8s.io/kubelet/pkg/apis/podresources/v1"
)

const (
	defaultSocketPath = "/var/lib/kubelet/pod-resources/kubelet.sock"
	gpuResourceName   = "nvidia.com/gpu"
)

// PodGPUMapping maps GPU UUID to pod information.
type PodGPUMapping struct {
	Namespace string
	PodName   string
	Container string
}

// PodMapper is the interface for getting GPU-to-pod mappings.
type PodMapper interface {
	GetGPUPodMapping(ctx context.Context) (map[string]PodGPUMapping, error)
}

// Client queries the kubelet Pod Resources API.
type Client struct {
	socketPath string
}

// NewClient creates a new Pod Resources API client.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	return &Client{socketPath: socketPath}
}

// GetGPUPodMapping returns a map of GPU device ID -> pod info for all GPUs
// currently allocated to pods on this node.
func (c *Client) GetGPUPodMapping(ctx context.Context) (map[string]PodGPUMapping, error) {
	conn, err := grpc.NewClient(
		"unix://"+c.socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "unix", c.socketPath)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to pod resources API: %w", err)
	}
	defer conn.Close()

	client := podresourcesv1.NewPodResourcesListerClient(conn)

	resp, err := client.List(ctx, &podresourcesv1.ListPodResourcesRequest{})
	if err != nil {
		return nil, fmt.Errorf("listing pod resources: %w", err)
	}

	mapping := make(map[string]PodGPUMapping)
	for _, pod := range resp.GetPodResources() {
		for _, container := range pod.GetContainers() {
			for _, device := range container.GetDevices() {
				if device.GetResourceName() != gpuResourceName {
					continue
				}
				for _, id := range device.GetDeviceIds() {
					mapping[id] = PodGPUMapping{
						Namespace: pod.GetNamespace(),
						PodName:   pod.GetName(),
						Container: container.GetName(),
					}
				}
			}
		}
	}
	return mapping, nil
}
