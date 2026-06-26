package main

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	pb "github.com/jia-gao/kube-gpu-top/api/gpuagent"
)

const (
	agentPort          = 9401
	agentNamespace     = "kube-gpu-system"
	agentLabelSelector = "app=kube-gpu-agent"
)

// loadRESTConfig resolves a Kubernetes REST config using the same precedence
// as kubectl: an explicit --kubeconfig path wins, otherwise the $KUBECONFIG
// environment variable (a path or os.PathListSeparator-joined list, merged in
// order), otherwise ~/.kube/config.
func loadRESTConfig(kubeconfig string) (*rest.Config, error) {
	// NewDefaultClientConfigLoadingRules() already honors $KUBECONFIG and
	// falls back to the recommended home file (~/.kube/config).
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules, &clientcmd.ConfigOverrides{},
	).ClientConfig()
}

// buildClientset constructs a Kubernetes clientset, resolving the kubeconfig
// via loadRESTConfig (explicit flag > $KUBECONFIG > ~/.kube/config).
func buildClientset(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := loadRESTConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// queryAllAgents fans out GetGPUStatus to every kube-gpu-agent pod and
// returns the successful responses. Failed agents are logged and skipped
// so a single broken node does not take down the whole command.
func queryAllAgents(ctx context.Context, clientset *kubernetes.Clientset) ([]*pb.GPUStatusResponse, error) {
	pods, err := clientset.CoreV1().Pods(agentNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: agentLabelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list agent pods: %w", err)
	}

	responses := make([]*pb.GPUStatusResponse, 0, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.Status.PodIP == "" {
			continue
		}
		addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, agentPort)
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			log.Printf("warning: dial %s (%s): %v", pod.Spec.NodeName, addr, err)
			continue
		}
		client := pb.NewGPUAgentServiceClient(conn)
		resp, err := client.GetGPUStatus(ctx, &pb.GPUStatusRequest{})
		conn.Close()
		if err != nil {
			log.Printf("warning: GetGPUStatus %s: %v", pod.Spec.NodeName, err)
			continue
		}
		responses = append(responses, resp)
	}
	return responses, nil
}
