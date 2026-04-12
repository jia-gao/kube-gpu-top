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
)

func main() {
	args := os.Args[1:]
	sub := "top"
	if len(args) > 0 {
		switch args[0] {
		case "top":
			sub, args = "top", args[1:]
		case "waste":
			sub, args = "waste", args[1:]
		case "-h", "--help", "help":
			printUsage()
			return
		}
	}

	switch sub {
	case "waste":
		runWaste(args)
	default:
		runTop(args)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: kubectl gpu-top <command> [flags]

Commands:
  top      Show GPU utilization per pod across the cluster (default)
  waste    Detect wasted GPUs and estimate their cost

Run 'kubectl gpu-top <command> -h' for command-specific flags.`)
}

// runTop fetches a single snapshot from every agent and prints a table.
func runTop(args []string) {
	fs := flag.NewFlagSet("top", flag.ExitOnError)
	namespace := fs.String("namespace", "", "filter by namespace (default: all)")
	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientset, err := buildClientset(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to build kubeconfig: %v", err)
	}

	responses, err := queryAllAgents(ctx, clientset)
	if err != nil {
		log.Fatalf("Failed to query agents: %v", err)
	}
	if len(responses) == 0 {
		fmt.Println("No kube-gpu-agent pods found. Is the DaemonSet deployed?")
		fmt.Println("  kubectl apply -f https://github.com/jia-gao/kube-gpu-top/deploy/daemonset.yaml")
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "NODE\tNAMESPACE\tPOD\tGPU\tUTIL\tMEM USED\tMEM TOTAL\tTEMP\tPOWER\n")

	for _, resp := range responses {
		for _, dev := range resp.Devices {
			podNs, podName := "-", "-"
			if dev.Pod != nil {
				podNs = dev.Pod.Namespace
				podName = dev.Pod.Name
			}
			if *namespace != "" && podNs != *namespace {
				continue
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d%%\t%s\t%s\t%d°C\t%dW\n",
				resp.NodeName,
				podNs,
				podName,
				shortGPUName(dev.Name),
				dev.GpuUtilization,
				formatBytes(dev.MemUsedBytes),
				formatBytes(dev.MemTotalBytes),
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
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return parts[0] + "-" + parts[len(parts)-1]
	}
	return name
}

// formatBytes converts bytes to a human-readable GiB/MiB string.
func formatBytes(b uint64) string {
	gib := float64(b) / (1024 * 1024 * 1024)
	if gib >= 1.0 {
		return fmt.Sprintf("%.1f GiB", gib)
	}
	mib := float64(b) / (1024 * 1024)
	return fmt.Sprintf("%.0f MiB", mib)
}
