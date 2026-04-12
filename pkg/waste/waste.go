// Package waste detects wasted GPUs from a series of samples and attaches
// an estimated dollar cost to each finding.
//
// It is intentionally pure: no Kubernetes or NVML imports. Callers collect
// samples however they like (e.g. by polling kube-gpu-agent over gRPC) and
// pass them to Analyze.
package waste

import (
	"fmt"
	"sort"
	"strings"
)

// Sample is a single point-in-time observation of one GPU.
type Sample struct {
	NodeName       string
	GPUUUID        string
	GPUName        string
	GPUUtilization uint32 // 0-100
	MemUsedBytes   uint64
	MemTotalBytes  uint64
	PowerWatts     uint32
	PodNamespace   string // empty if GPU is not bound to a pod
	PodName        string
}

// Thresholds controls what counts as waste.
type Thresholds struct {
	// UtilPercent: avg GPU SM utilization below this is considered idle.
	UtilPercent uint32
	// MemPercent: avg memory-used as percent of total below this is considered idle.
	MemPercent uint32
}

// DefaultThresholds are conservative defaults used by the CLI.
// 5% util + 10% mem matches "effectively idle": weights may be loaded but
// no real compute is happening.
var DefaultThresholds = Thresholds{UtilPercent: 5, MemPercent: 10}

// Reason describes why a GPU was flagged.
type Reason string

const (
	// ReasonIdle means both compute and memory are effectively unused.
	ReasonIdle Reason = "idle"
	// ReasonComputeIdle means compute is unused but memory is held
	// (e.g. a stale model loaded into VRAM).
	ReasonComputeIdle Reason = "compute-idle"
)

// Finding is a single wasted GPU, with enough context for the report.
type Finding struct {
	NodeName     string
	GPUUUID      string
	GPUName      string
	PodNamespace string
	PodName      string
	AvgUtil      float64
	AvgMemPct    float64
	AvgPowerW    float64
	HourlyUSD    float64
	Reason       Reason
}

// MonthlyUSD is HourlyUSD * 24 * 30 for a rough monthly projection.
func (f Finding) MonthlyUSD() float64 { return f.HourlyUSD * 24 * 30 }

// Totals summarizes a list of findings.
type Totals struct {
	Count      int
	HourlyUSD  float64
	MonthlyUSD float64
}

// Analyze aggregates samples per GPU, applies thresholds, and returns
// findings sorted by monthly cost (highest first).
func Analyze(samples []Sample, t Thresholds, rates CostTable) []Finding {
	type key struct{ node, uuid string }
	type agg struct {
		node, uuid, name, ns, pod string
		count                     int
		sumUtil                   uint64
		sumMemPct                 float64
		sumPower                  uint64
	}

	byGPU := make(map[key]*agg)
	for _, s := range samples {
		// Only pod-attached GPUs count as waste: an unclaimed GPU is
		// either the scheduler's problem or a node-drain artifact.
		if s.PodName == "" {
			continue
		}
		k := key{s.NodeName, s.GPUUUID}
		a, ok := byGPU[k]
		if !ok {
			a = &agg{
				node: s.NodeName, uuid: s.GPUUUID, name: s.GPUName,
				ns: s.PodNamespace, pod: s.PodName,
			}
			byGPU[k] = a
		}
		a.count++
		a.sumUtil += uint64(s.GPUUtilization)
		if s.MemTotalBytes > 0 {
			a.sumMemPct += float64(s.MemUsedBytes) / float64(s.MemTotalBytes) * 100
		}
		a.sumPower += uint64(s.PowerWatts)
	}

	var findings []Finding
	for _, a := range byGPU {
		if a.count == 0 {
			continue
		}
		avgUtil := float64(a.sumUtil) / float64(a.count)
		avgMem := a.sumMemPct / float64(a.count)
		avgPow := float64(a.sumPower) / float64(a.count)

		var reason Reason
		switch {
		case avgUtil < float64(t.UtilPercent) && avgMem < float64(t.MemPercent):
			reason = ReasonIdle
		case avgUtil < float64(t.UtilPercent):
			reason = ReasonComputeIdle
		default:
			continue
		}

		findings = append(findings, Finding{
			NodeName:     a.node,
			GPUUUID:      a.uuid,
			GPUName:      a.name,
			PodNamespace: a.ns,
			PodName:      a.pod,
			AvgUtil:      avgUtil,
			AvgMemPct:    avgMem,
			AvgPowerW:    avgPow,
			HourlyUSD:    rates.Lookup(a.name),
			Reason:       reason,
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		return findings[i].MonthlyUSD() > findings[j].MonthlyUSD()
	})
	return findings
}

// Summarize produces headline totals across all findings.
func Summarize(findings []Finding) Totals {
	t := Totals{Count: len(findings)}
	for _, f := range findings {
		t.HourlyUSD += f.HourlyUSD
	}
	t.MonthlyUSD = t.HourlyUSD * 24 * 30
	return t
}

// CostTable maps a GPU model substring to an hourly USD rate.
// Lookup is case-insensitive and picks the longest matching key so more
// specific entries (e.g. "A100-80GB") take precedence over shorter ones
// ("A100") when both are present.
type CostTable map[string]float64

// DefaultCostTable uses rough 2026 on-demand cloud rates as a proxy for
// "a dollar figure the reader will recognize". Users running on-prem
// should pass --hourly-rate to override with an amortized number.
var DefaultCostTable = CostTable{
	"H100": 5.00,
	"A100": 2.50,
	"L40":  1.80,
	"L4":   0.70,
	"V100": 1.50,
	"T4":   0.35,
	"A10G": 1.20,
}

// Lookup returns the hourly rate for a GPU name, or 0 if no entry matches.
func (c CostTable) Lookup(gpuName string) float64 {
	name := strings.ToUpper(strings.TrimPrefix(gpuName, "NVIDIA "))
	var bestKey string
	var bestRate float64
	for k, v := range c {
		upk := strings.ToUpper(k)
		if strings.Contains(name, upk) && len(upk) > len(bestKey) {
			bestKey = upk
			bestRate = v
		}
	}
	return bestRate
}

// FormatReport renders findings as a human-readable table.
func FormatReport(findings []Finding, totals Totals) string {
	var b strings.Builder
	if len(findings) == 0 {
		b.WriteString("No wasted GPUs detected.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "WASTED GPUS: %d    EST HOURLY: $%.2f    EST MONTHLY: $%.0f\n\n",
		totals.Count, totals.HourlyUSD, totals.MonthlyUSD)
	fmt.Fprintf(&b, "%-14s  %-14s  %-28s  %-12s  %8s  %8s  %10s  %s\n",
		"NODE", "NAMESPACE", "POD", "GPU", "AVG UTIL", "AVG MEM", "EST $/MO", "REASON")
	for _, f := range findings {
		fmt.Fprintf(&b, "%-14s  %-14s  %-28s  %-12s  %7.1f%%  %7.1f%%  %10s  %s\n",
			trunc(f.NodeName, 14),
			trunc(f.PodNamespace, 14),
			trunc(f.PodName, 28),
			trunc(shortGPU(f.GPUName), 12),
			f.AvgUtil,
			f.AvgMemPct,
			fmt.Sprintf("$%.0f", f.MonthlyUSD()),
			f.Reason,
		)
	}
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 2 {
		return s[:n]
	}
	return s[:n-2] + ".."
}

func shortGPU(name string) string {
	name = strings.TrimPrefix(name, "NVIDIA ")
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return parts[0] + "-" + parts[len(parts)-1]
	}
	return name
}
