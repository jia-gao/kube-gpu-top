# Launch blog post — outline

Working title: **"We found $40,000 of idle GPUs in our Kubernetes cluster. So we built a tool."**

Target venues: personal blog → Hacker News (Show HN) → r/kubernetes → r/devops → LinkedIn → CNCF Slack #kubernetes-users.

## Why this framing works

- **Dollar figures travel.** HN readers click "we wasted $X" far more reliably than "new CLI tool announcement".
- **The number is defensible.** A single idle A100 at cloud rates is ~$1,800/month. Ten idle GPUs = $18k/mo = $216k/yr. Readers don't need to trust the tool to trust that number.
- **It gives the tool a *job to be done*,** not just a feature list. "htop for GPUs" is the shape; "find wasted GPUs" is the verb.
- **It's honest.** The first version of the tool was literally built to answer this question on a real cluster.

## Structure (target: 1200–1600 words, ~6 min read)

### 1. Cold open (150 words)
The moment that started it. One paragraph, concrete. Something like:
> "Last month I was staring at our GPU billing dashboard wondering why it kept climbing. kubectl top gave me CPU and memory. nvidia-smi gave me per-GPU numbers but no idea which pod was using what. Grafana had the data — three clicks and a PromQL query away. So I wrote a one-liner to find every GPU in the cluster that was sitting below 5% utilization. It found eleven. At on-demand rates, that's about $40,000 a month."

No throat-clearing. No "in this post I will…". Drop the reader straight into the problem.

### 2. Why this is hard today (200 words)
Table of existing tools and what each one is missing:

| Tool | What it shows | What it's missing |
| --- | --- | --- |
| `kubectl top` | CPU, memory | GPUs don't exist |
| `nvidia-smi` | Per-GPU metrics | No pod/namespace awareness |
| `nvtop` / `nvitop` | Beautiful single-node view | No cluster view |
| DCGM + Prometheus + Grafana | Everything | Requires a whole observability stack |

The gap: a zero-config, single-binary way to answer *"which pod is using which GPU, and is anyone wasting one right now?"*

### 3. What we built (300 words)
Show the tool before explaining it. Two command-line examples first, narrative second.

```
$ kubectl gpu-top
NODE          NAMESPACE   POD                    GPU       UTIL   MEM      TEMP   POWER
gpu-node-01   ml-team     train-llama-70b-0      A100-80GB 94%    71.2 GiB  72°C  298W
gpu-node-02   inference   vllm-serve-7b-abc12    A100-80GB 45%    14.2 GiB  58°C  165W
gpu-node-03   dev         notebook-alice-gpu     A100-80GB  3%     2.1 GiB  35°C   62W
```

```
$ kubectl gpu-top waste
Sampling GPU utilization for 60s (every 5s)...
WASTED GPUS: 11    EST HOURLY: $27.50    EST MONTHLY: $19,800

NODE          NAMESPACE  POD                  GPU        AVG UTIL  AVG MEM   EST $/MO   REASON
gpu-node-03   dev        notebook-alice-gpu   A100-80GB      1.2%     2.1%     $1,800   idle
gpu-node-05   inference  vllm-idle-7b         A100-80GB      0.8%    91.4%     $1,800   compute-idle
...
```

Then a short architecture paragraph: DaemonSet agent on each GPU node, go-nvml for metrics, kubelet Pod Resources API for pod attribution, gRPC to a kubectl plugin. No Prometheus, no Grafana, no CRDs.

One diagram (the existing ASCII one from the README is fine — keep it lightweight).

### 4. The two categories of waste we care about (250 words)
Make the distinction real:

- **Idle** — a GPU held by a pod but with no compute *and* no meaningful memory in use. Almost always a forgotten notebook, a stopped training job whose pod didn't exit, or a dev environment left running over the weekend.
- **Compute-idle** — a GPU pinned by a pod that has loaded a model into VRAM but isn't serving any requests. This is the sneakier one: a vLLM replica that hasn't seen traffic in hours still looks "used" because the weights are resident. You can only spot it with a rolling window, which is why `kubectl gpu-top waste` polls for 60 seconds by default rather than snapshotting.

Call out what *isn't* waste: underprovisioned autoscalers, warm replicas you're deliberately keeping hot, multi-tenant GPU time-slicing. The tool flags candidates; a human decides.

### 5. How we estimate cost (150 words)
Honest disclosure: the dollar figures come from a lookup table of rough 2026 on-demand cloud rates, which is the number most readers have a mental model for. Link to the table in the source code. Show the `--hourly-rate` override for users who are running on-prem and want amortized cost instead. Explicitly say: "these numbers are a conversation starter, not an audit."

### 6. Installing it (100 words)
Three commands, nothing else:

```
kubectl krew install gpu-top
kubectl apply -f https://raw.githubusercontent.com/jia-gao/kube-gpu-top/main/deploy/daemonset.yaml
kubectl gpu-top waste
```

### 7. What's next (150 words)
- Interactive TUI (bubbletea) for a live `htop`-style view.
- MIG and time-slicing support (per-instance attribution, not just per-device).
- Historical mode that reads from an existing Prometheus instead of polling, for clusters that already have DCGM.
- Slack/webhook alerts: "this pod has been idle on an H100 for 6 hours."

End with an ask: *"If you run a GPU cluster on Kubernetes and you try this, I'd love to hear what number `kubectl gpu-top waste` prints for you. Open an issue or reach out."*

## Launch-day checklist

- [ ] Blog post published on personal site with canonical URL
- [ ] Asciinema recording of `kubectl gpu-top` and `kubectl gpu-top waste` embedded at the top of the README
- [ ] v0.1.0 release cut on GitHub with prebuilt binaries for linux/darwin × amd64/arm64
- [ ] Krew manifest PR opened against `kubernetes-sigs/krew-index`
- [ ] `USERS.md` file in the repo with a template for adopters to PR their org name
- [ ] GitHub Discussions enabled
- [ ] Show HN post scheduled for Tue/Wed 8:30am PT (best HN window)
- [ ] r/kubernetes post (different title; HN-style titles die on Reddit)
- [ ] LinkedIn post tagging llm-d maintainers and Red Hat / NVIDIA / Google contacts
- [ ] CNCF Slack posts in #kubernetes-users and #sig-node
- [ ] Screenshot of GitHub traffic insights dashboard saved for NIW evidence folder

## NIW evidence to capture post-launch

- GitHub stars-over-time graph (weekly screenshots)
- Krew install counts (queryable from krew-index metrics)
- `USERS.md` entries (each one is a citable adopter)
- HN comment thread (archive the page)
- Reddit thread upvotes + comment count
- Any downstream blog posts, tweets, or conference mentions
- Issues filed by users at identifiable companies (these are the gold: evidence the tool is used in production at orgs you don't control)
