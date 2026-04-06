package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	pb "github.com/jiazhougao/kube-gpu-top/api/gpuagent"
)

const agentPort = 9401

func main() {
	namespace := flag.String("namespace", "", "filter by namespace (default: all)")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig (default: in-cluster or ~/.kube/config)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Build K8s client to discover agent pods
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		// Try default kubeconfig path
		home, _ := os.UserHomeDir()
		config, err = clientcmd.BuildConfigFromFlags("", home+"/.kube/config")
		if err != nil {
			log.Fatalf("Failed to build kubeconfig: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create K8s client: %v", err)
	}

	// Find agent pods by label
	pods, err := clientset.CoreV1().Pods("kube-gpu-system").List(ctx, metav1.ListOptions{
		LabelSelector: "app=kube-gpu-agent",
	})
	if err != nil {
		log.Fatalf("Failed to list agent pods: %v", err)
	}

	if len(pods.Items) == 0 {
		fmt.Println("No kube-gpu-agent pods found. Is the DaemonSet deployed?")
		fmt.Println("  kubectl apply -f https://github.com/jiazhougao/kube-gpu-top/deploy/daemonset.yaml")
		os.Exit(1)
	}

	// Query each agent and collect results
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "NODE\tNAMESPACE\tPOD\tGPU\tUTIL\tMEM USED\tMEM TOTAL\tTEMP\tPOWER\n")

	for _, pod := range pods.Items {
		agentAddr := fmt.Sprintf("%s:%d", pod.Status.PodIP, agentPort)

		conn, err := grpc.NewClient(agentAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Printf("Warning: failed to connect to agent on %s: %v", pod.Spec.NodeName, err)
			continue
		}

		client := pb.NewGPUAgentServiceClient(conn)
		resp, err := client.GetGPUStatus(ctx, &pb.GPUStatusRequest{})
		conn.Close()
		if err != nil {
			log.Printf("Warning: failed to get GPU status from %s: %v", pod.Spec.NodeName, err)
			continue
		}

		for _, dev := range resp.Devices {
			podNs := "-"
			podName := "-"
			if dev.Pod != nil {
				podNs = dev.Pod.Namespace
				podName = dev.Pod.Name
			}

			// Filter by namespace if specified
			if *namespace != "" && podNs != *namespace {
				continue
			}

			gpuName := shortGPUName(dev.Name)
			memUsed := formatBytes(dev.MemUsedBytes)
			memTotal := formatBytes(dev.MemTotalBytes)

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d%%\t%s\t%s\t%d°C\t%dW\n",
				resp.NodeName,
				podNs,
				podName,
				gpuName,
				dev.GpuUtilization,
				memUsed,
				memTotal,
				dev.TemperatureC,
				dev.PowerWatts,
			)
		}
	}
	w.Flush()
}

// shortGPUName shortens GPU names like "NVIDIA A100-SXM4-80GB" to "A100-80GB".
func shortGPUName(name string) string {
	name = strings.TrimPrefix(name, "NVIDIA ")
	// Keep model and memory size, drop form factor
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return parts[0] + "-" + parts[len(parts)-1]
	}
	return name
}

// formatBytes converts bytes to human-readable GiB string.
func formatBytes(b uint64) string {
	gib := float64(b) / (1024 * 1024 * 1024)
	if gib >= 1.0 {
		return fmt.Sprintf("%.1f GiB", gib)
	}
	mib := float64(b) / (1024 * 1024)
	return fmt.Sprintf("%.0f MiB", mib)
}
