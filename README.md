# kube-gpu-top

**The missing `kubectl top` for GPUs.**

<p align="center">
  <img src="assets/demo.svg" alt="kube-gpu-top demo" width="800">
</p>

One command. Every GPU across every node. Pod-level attribution. No dashboards required.

## Why?

Checking GPU utilization in Kubernetes today is harder than it should be:

- **`kubectl top`** only shows CPU and memory. GPUs don't exist.
- **`nvidia-smi`** shows GPU metrics but has no concept of pods, namespaces, or workloads.
- **`nvtop` / `nvitop`** are great single-node tools but don't work across a cluster.
- **DCGM + Prometheus + Grafana** gives you everything, but requires deploying and maintaining a full observability stack just to answer "which pod is using my GPU?"

`kube-gpu-top` fills this gap. It's a single binary CLI backed by a lightweight DaemonSet agent. No Prometheus. No Grafana. Just a terminal command.

## Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  User Machine                                                │
│                                                              │
│  kubectl gpu-top ──── K8s API ── discover agent pods         │
│         │                                                    │
└─────────┼────────────────────────────────────────────────────┘
          │ gRPC :9401
          ▼
┌──────────────────────────────────────────────────────────────┐
│  GPU Node (DaemonSet: kube-gpu-agent)                        │
│                                                              │
│  ┌─────────────────┐       ┌──────────────────────────────┐  │
│  │   go-nvml       │       │  kubelet Pod Resources API   │  │
│  │                 │       │  /var/lib/kubelet/           │  │
│  │  GPU UUID       │       │  pod-resources/kubelet.sock  │  │
│  │  Utilization    │       │                              │  │
│  │  Memory         │       │  GPU UUID ──► Pod/Namespace  │  │
│  │  Temperature    │       │                              │  │
│  │  Power          │       └──────────────┬───────────────┘  │
│  └────────┬────────┘                      │                  │
│           │            JOIN on GPU UUID   │                  │
│           └────────────────┬──────────────┘                  │
│                            ▼                                 │
│                   GPUStatusResponse                          │
│            (metrics + pod attribution)                       │
└──────────────────────────────────────────────────────────────┘
```

The agent runs on each GPU node and does two things:
1. Queries **NVML** (via [go-nvml](https://github.com/NVIDIA/go-nvml)) for real-time GPU metrics
2. Queries the **kubelet Pod Resources API** to map each GPU UUID to its owning pod

It joins the two by GPU UUID and serves the result over gRPC. The CLI discovers agents via the Kubernetes API, fans out gRPC calls, and renders the table.

## Quick Start

**1. Deploy the agent DaemonSet:**

```bash
# Option A: Via Helm
helm install kube-gpu-top oci://ghcr.io/jia-gao/charts/kube-gpu-top

# Option B: Plain manifest
kubectl apply -f https://raw.githubusercontent.com/jia-gao/kube-gpu-top/main/deploy/daemonset.yaml
```

The agent runs only on nodes with `nvidia.com/gpu.present=true` and requests minimal resources (10m CPU, 32Mi memory).

Helm lets you customize the NVIDIA driver path, resource limits, and tolerations via `values.yaml`. See [charts/kube-gpu-top/values.yaml](charts/kube-gpu-top/values.yaml) for all options.

**2. Install the CLI:**

```bash
# Option A: Via Krew (recommended)
kubectl krew install gpu-top

# Option B: Download the prebuilt binary
curl -sL https://github.com/jia-gao/kube-gpu-top/releases/latest/download/kubectl-gpu-top_v0.1.0_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz | tar xz
sudo mv kubectl-gpu-top /usr/local/bin/

# Option C: Via Go
go install github.com/jia-gao/kube-gpu-top/cmd/kubectl-gpu-top@latest
```

**3. See every GPU in your cluster:**

```bash
kubectl gpu-top
```

**4. Find wasted GPUs with cost estimates:**

```bash
kubectl gpu-top waste
```

This polls all GPUs for 60 seconds and flags any that are idle or holding memory but not computing. Output includes an estimated monthly cost per wasted GPU.

**Options:**

```bash
kubectl gpu-top top --namespace ml-team          # filter by namespace
kubectl gpu-top waste --duration 5m              # longer sampling window
kubectl gpu-top waste --util-threshold 10        # flag GPUs below 10% util
kubectl gpu-top waste --hourly-rate 1.20         # override cost per GPU-hour
```

## Building from Source

```bash
git clone https://github.com/jia-gao/kube-gpu-top.git
cd kube-gpu-top

# Build both CLI and agent
make build

# Build only the CLI
make build-cli

# Build the agent container image
make docker-build

# Run tests
make test
```

Binaries are output to `bin/`.

## Requirements

- Kubernetes 1.20+
- **NVIDIA GPUs** with drivers installed on worker nodes (AMD/Intel GPU support is on the roadmap — see [#1](https://github.com/jia-gao/kube-gpu-top/issues/1))
- [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin) deployed (standard in most GPU clusters)

## Roadmap

- [x] Core agent with go-nvml + Pod Resources API
- [x] CLI table output with namespace filtering
- [x] Waste detection with cost estimates (`kubectl gpu-top waste`)
- [x] Krew plugin manifest
- [x] Prebuilt binaries (linux/darwin × amd64/arm64)
- [x] Multi-arch agent container image
- [x] Helm chart
- [ ] AMD/Intel GPU support ([#1](https://github.com/jia-gao/kube-gpu-top/issues/1))
- [ ] Interactive TUI mode (bubbletea)
- [ ] Time-slicing and MIG support
- [ ] Historical mode (read from Prometheus instead of polling)
- [ ] Slack / webhook alerts for idle GPUs

## License

Apache 2.0
