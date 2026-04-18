package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	pb "github.com/jia-gao/kube-gpu-top/api/gpuagent"
	"github.com/jia-gao/kube-gpu-top/pkg/tui"
)

// runTUI launches the interactive bubbletea TUI.
func runTUI(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")
	namespace := fs.String("namespace", "", "initial namespace filter (default: all)")
	interval := fs.Duration("interval", 2*time.Second, "data refresh interval")
	_ = fs.Parse(args)

	clientset, err := buildClientset(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to build kubeconfig: %v", err)
	}

	queryFn := func() ([]*pb.GPUStatusResponse, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return queryAllAgents(ctx, clientset)
	}

	m := tui.New(queryFn, *interval)
	if *namespace != "" {
		m.SetNamespaceFilter(*namespace)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
