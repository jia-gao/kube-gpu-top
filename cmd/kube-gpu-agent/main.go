package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	pb "github.com/jiazhougao/kube-gpu-top/api/gpuagent"
	"github.com/jiazhougao/kube-gpu-top/pkg/agent"
	"github.com/jiazhougao/kube-gpu-top/pkg/gpu"
)

func main() {
	port := flag.Int("port", 9401, "gRPC listen port")
	podResourcesSocket := flag.String("pod-resources-socket", "", "path to kubelet pod-resources socket")
	flag.Parse()

	if err := gpu.Init(); err != nil {
		log.Fatalf("Failed to initialize NVML: %v", err)
	}
	defer gpu.Shutdown()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("Failed to listen on port %d: %v", *port, err)
	}

	grpcServer := grpc.NewServer()
	srv := agent.NewServer(*podResourcesSocket)
	pb.RegisterGPUAgentServiceServer(grpcServer, srv)

	// Graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		log.Println("Shutting down...")
		grpcServer.GracefulStop()
	}()

	log.Printf("kube-gpu-agent listening on :%d", *port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC server error: %v", err)
	}
}
