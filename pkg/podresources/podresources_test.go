package podresources

import "testing"

// TestClientImplementsInterface verifies that *Client satisfies PodMapper.
func TestClientImplementsInterface(t *testing.T) {
	var _ PodMapper = (*Client)(nil)
}

// Note: GetGPUPodMapping() requires a running kubelet with the Pod Resources API socket.
// Integration tests should be run on a real Kubernetes node.
