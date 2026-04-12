package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/jia-gao/kube-gpu-top/pkg/waste"
)

// runWaste polls every kube-gpu-agent for a rolling window, averages each
// GPU's utilization, and prints a "wasted GPUs" report with estimated cost.
func runWaste(args []string) {
	fs := flag.NewFlagSet("waste", flag.ExitOnError)
	duration := fs.Duration("duration", 60*time.Second, "rolling window to sample over")
	interval := fs.Duration("interval", 5*time.Second, "sampling interval")
	utilThreshold := fs.Uint("util-threshold", 5, "flag GPUs with avg util below this percent")
	memThreshold := fs.Uint("mem-threshold", 10, "flag GPUs with avg mem-used below this percent")
	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig")
	hourlyRate := fs.Float64("hourly-rate", 0, "override USD/hour for every flagged GPU (ignores built-in table)")
	_ = fs.Parse(args)

	clientset, err := buildClientset(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to build k8s client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration+30*time.Second)
	defer cancel()

	fmt.Fprintf(os.Stderr, "Sampling GPU utilization for %s (every %s)...\n", *duration, *interval)

	samples := collectSamples(ctx, clientset, *duration, *interval)
	if len(samples) == 0 {
		fmt.Println("No samples collected. Is the DaemonSet deployed?")
		fmt.Println("  kubectl apply -f https://github.com/jia-gao/kube-gpu-top/deploy/daemonset.yaml")
		os.Exit(1)
	}

	t := waste.Thresholds{
		UtilPercent: uint32(*utilThreshold),
		MemPercent:  uint32(*memThreshold),
	}
	findings := waste.Analyze(samples, t, waste.DefaultCostTable)
	if *hourlyRate > 0 {
		for i := range findings {
			findings[i].HourlyUSD = *hourlyRate
		}
	}
	totals := waste.Summarize(findings)
	fmt.Print(waste.FormatReport(findings, totals))
}

// collectSamples polls the cluster for the given duration and returns one
// Sample per (GPU, tick). Failed queries are logged and skipped.
func collectSamples(ctx context.Context, clientset *kubernetes.Clientset, duration, interval time.Duration) []waste.Sample {
	var samples []waste.Sample

	tick := func() {
		responses, err := queryAllAgents(ctx, clientset)
		if err != nil {
			log.Printf("warning: query agents: %v", err)
			return
		}
		for _, resp := range responses {
			for _, dev := range resp.Devices {
				s := waste.Sample{
					NodeName:       resp.NodeName,
					GPUUUID:        dev.Uuid,
					GPUName:        dev.Name,
					GPUUtilization: dev.GpuUtilization,
					MemUsedBytes:   dev.MemUsedBytes,
					MemTotalBytes:  dev.MemTotalBytes,
					PowerWatts:     dev.PowerWatts,
				}
				if dev.Pod != nil {
					s.PodNamespace = dev.Pod.Namespace
					s.PodName = dev.Pod.Name
				}
				samples = append(samples, s)
			}
		}
	}

	// First sample immediately so a duration < interval still produces data.
	tick()
	deadline := time.Now().Add(duration)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return samples
		case <-ticker.C:
			if time.Now().After(deadline) {
				return samples
			}
			tick()
		}
	}
}
