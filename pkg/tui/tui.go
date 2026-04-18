// Package tui provides an interactive terminal UI for kube-gpu-top using
// bubbletea. It is intentionally decoupled from Kubernetes and gRPC: callers
// inject a QueryFunc that returns GPU status responses, keeping this package
// testable without a live cluster.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	pb "github.com/jia-gao/kube-gpu-top/api/gpuagent"
)

// QueryFunc fetches GPU status from all agents. Callers provide the real
// implementation; tests can substitute a stub.
type QueryFunc func() ([]*pb.GPUStatusResponse, error)

// SortColumn identifies which column the table is sorted by.
type SortColumn int

const (
	SortUtil SortColumn = iota
	SortMem
	SortPower
	SortNode
	SortNamespace
)

func (s SortColumn) String() string {
	switch s {
	case SortUtil:
		return "UTIL"
	case SortMem:
		return "MEM"
	case SortPower:
		return "POWER"
	case SortNode:
		return "NODE"
	case SortNamespace:
		return "NAMESPACE"
	default:
		return "?"
	}
}

// NextSort cycles to the next sort column.
func (s SortColumn) Next() SortColumn {
	return (s + 1) % 5
}

// InputMode tracks what the user is typing into.
type InputMode int

const (
	ModeNormal InputMode = iota
	ModeNamespace
	ModeSearch
)

// Row is a flattened, display-ready GPU row.
type Row struct {
	Node      string
	Namespace string
	Pod       string
	GPU       string
	Util      uint32
	MemUsed   uint64
	MemTotal  uint64
	Temp      uint32
	Power     uint32
}

// tickMsg triggers a data refresh.
type tickMsg time.Time

// dataMsg carries refreshed GPU data.
type dataMsg struct {
	rows      []Row
	gpuCount  int
	nodeCount int
	err       error
}

// Model is the bubbletea model for kube-gpu-top's interactive TUI.
type Model struct {
	// Configuration
	Interval time.Duration
	Query    QueryFunc

	// Data
	rows      []Row
	gpuCount  int
	nodeCount int
	lastErr   error
	lastFetch time.Time

	// View state
	sortCol   SortColumn
	cursor    int
	offset    int // scroll offset
	termWidth int
	termHeight int

	// Filtering
	inputMode       InputMode
	namespaceFilter string
	searchFilter    string
	inputBuf        string
}

// New creates a Model with the given query function and refresh interval.
func New(query QueryFunc, interval time.Duration) Model {
	return Model{
		Interval:   interval,
		Query:      query,
		sortCol:    SortUtil,
		termWidth:  120,
		termHeight: 24,
	}
}

// SetNamespaceFilter sets the initial namespace filter.
func (m *Model) SetNamespaceFilter(ns string) {
	m.namespaceFilter = ns
}

// Init starts the first tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchData(),
		m.tickCmd(),
	)
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.Interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) fetchData() tea.Cmd {
	query := m.Query
	return func() tea.Msg {
		responses, err := query()
		if err != nil {
			return dataMsg{err: err}
		}
		rows, gpuCount, nodeCount := flattenResponses(responses)
		return dataMsg{rows: rows, gpuCount: gpuCount, nodeCount: nodeCount}
	}
}

func flattenResponses(responses []*pb.GPUStatusResponse) ([]Row, int, int) {
	var rows []Row
	nodeSet := make(map[string]struct{})
	gpuCount := 0
	for _, resp := range responses {
		nodeSet[resp.NodeName] = struct{}{}
		for _, dev := range resp.Devices {
			gpuCount++
			podNs, podName := "-", "-"
			if dev.Pod != nil {
				podNs = dev.Pod.Namespace
				podName = dev.Pod.Name
			}
			rows = append(rows, Row{
				Node:      resp.NodeName,
				Namespace: podNs,
				Pod:       podName,
				GPU:       shortGPUName(dev.Name),
				Util:      dev.GpuUtilization,
				MemUsed:   dev.MemUsedBytes,
				MemTotal:  dev.MemTotalBytes,
				Temp:      dev.TemperatureC,
				Power:     dev.PowerWatts,
			})
		}
	}
	return rows, gpuCount, len(nodeSet)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termWidth = msg.Width
		m.termHeight = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.fetchData(), m.tickCmd())

	case dataMsg:
		m.lastFetch = time.Now()
		if msg.err != nil {
			m.lastErr = msg.err
		} else {
			m.lastErr = nil
			m.rows = msg.rows
			m.gpuCount = msg.gpuCount
			m.nodeCount = msg.nodeCount
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// In input mode, handle text entry.
	if m.inputMode != ModeNormal {
		switch msg.Type {
		case tea.KeyEnter:
			if m.inputMode == ModeNamespace {
				m.namespaceFilter = m.inputBuf
			} else {
				m.searchFilter = m.inputBuf
			}
			m.inputBuf = ""
			m.inputMode = ModeNormal
			m.cursor = 0
			m.offset = 0
		case tea.KeyEsc:
			m.inputBuf = ""
			m.inputMode = ModeNormal
		case tea.KeyBackspace:
			if len(m.inputBuf) > 0 {
				m.inputBuf = m.inputBuf[:len(m.inputBuf)-1]
			}
		default:
			if msg.Type == tea.KeyRunes {
				m.inputBuf += string(msg.Runes)
			}
		}
		return m, nil
	}

	// Normal mode key handling.
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "s":
		m.sortCol = m.sortCol.Next()
	case "n":
		m.inputMode = ModeNamespace
		m.inputBuf = m.namespaceFilter
	case "/":
		m.inputMode = ModeSearch
		m.inputBuf = m.searchFilter
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		filtered := m.filteredRows()
		if m.cursor < len(filtered)-1 {
			m.cursor++
		}
	}
	return m, nil
}

// filteredRows applies namespace and search filters, then sorts.
func (m Model) filteredRows() []Row {
	var filtered []Row
	for _, r := range m.rows {
		if m.namespaceFilter != "" && r.Namespace != m.namespaceFilter {
			continue
		}
		if m.searchFilter != "" && !strings.Contains(
			strings.ToLower(r.Pod), strings.ToLower(m.searchFilter)) {
			continue
		}
		filtered = append(filtered, r)
	}
	sortRows(filtered, m.sortCol)
	return filtered
}

func sortRows(rows []Row, col SortColumn) {
	sort.SliceStable(rows, func(i, j int) bool {
		switch col {
		case SortUtil:
			return rows[i].Util > rows[j].Util
		case SortMem:
			return rows[i].MemUsed > rows[j].MemUsed
		case SortPower:
			return rows[i].Power > rows[j].Power
		case SortNode:
			return rows[i].Node < rows[j].Node
		case SortNamespace:
			return rows[i].Namespace < rows[j].Namespace
		}
		return false
	})
}

// View renders the TUI.
func (m Model) View() string {
	var b strings.Builder

	// -- Header --
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	ts := m.lastFetch.Format("15:04:05")
	header := fmt.Sprintf("kube-gpu-top -- %d GPUs across %d nodes -- refreshing every %s  [%s]",
		m.gpuCount, m.nodeCount, m.Interval, ts)
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")

	if m.lastErr != nil {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		b.WriteString(errStyle.Render(fmt.Sprintf("Error: %v", m.lastErr)))
		b.WriteString("\n")
	}

	// -- Active filters --
	if m.namespaceFilter != "" || m.searchFilter != "" {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
		var parts []string
		if m.namespaceFilter != "" {
			parts = append(parts, "ns="+m.namespaceFilter)
		}
		if m.searchFilter != "" {
			parts = append(parts, "search="+m.searchFilter)
		}
		b.WriteString(dimStyle.Render("Filters: " + strings.Join(parts, "  ")))
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Sort: %s (press s to cycle)\n", m.sortCol))

	// -- Input prompt --
	if m.inputMode != ModeNormal {
		prompt := "namespace> "
		if m.inputMode == ModeSearch {
			prompt = "search> "
		}
		promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
		b.WriteString(promptStyle.Render(prompt+m.inputBuf+"_") + "\n")
	}

	// -- Table --
	rows := m.filteredRows()

	// Column header
	colHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	colHeader := fmt.Sprintf("  %-16s %-14s %-30s %-12s %-20s %9s %9s %6s %6s",
		"NODE", "NAMESPACE", "POD", "GPU", "UTIL", "MEM USED", "MEM TOTAL", "TEMP", "POWER")
	b.WriteString(colHeaderStyle.Render(colHeader))
	b.WriteString("\n")

	// Determine visible rows based on terminal height.
	// Reserve lines for header (1) + error (0-1) + filters (0-1) + sort (1) + col header (1) + footer (1) + prompt (0-1).
	reservedLines := 5
	if m.lastErr != nil {
		reservedLines++
	}
	if m.namespaceFilter != "" || m.searchFilter != "" {
		reservedLines++
	}
	if m.inputMode != ModeNormal {
		reservedLines++
	}
	visibleRows := m.termHeight - reservedLines
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Adjust scroll offset so cursor is always visible.
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visibleRows {
		m.offset = m.cursor - visibleRows + 1
	}

	for i := m.offset; i < len(rows) && i < m.offset+visibleRows; i++ {
		r := rows[i]
		pointer := "  "
		if i == m.cursor {
			pointer = "> "
		}

		utilBar := renderUtilBar(r.Util)
		line := fmt.Sprintf("%s%-16s %-14s %-30s %-12s %s %9s %9s %4dC %4dW",
			pointer,
			trunc(r.Node, 16),
			trunc(r.Namespace, 14),
			trunc(r.Pod, 30),
			trunc(r.GPU, 12),
			utilBar,
			formatBytes(r.MemUsed),
			formatBytes(r.MemTotal),
			r.Temp,
			r.Power,
		)

		if i == m.cursor {
			cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("236"))
			b.WriteString(cursorStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}

	// -- Footer --
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	footer := "q: quit  s: sort  n: filter namespace  /: search  j/k: scroll"
	b.WriteString(footerStyle.Render(footer))

	return b.String()
}

// renderUtilBar draws a visual bar like "[####------] 42%".
func renderUtilBar(util uint32) string {
	const barWidth = 10
	filled := int(util) * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	empty := barWidth - filled

	bar := strings.Repeat("#", filled) + strings.Repeat("-", empty)

	var color lipgloss.Color
	switch {
	case util >= 80:
		color = lipgloss.Color("196") // red
	case util >= 50:
		color = lipgloss.Color("214") // yellow
	default:
		color = lipgloss.Color("46") // green
	}

	style := lipgloss.NewStyle().Foreground(color)
	return style.Render(fmt.Sprintf("[%s] %3d%%", bar, util))
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

func formatBytes(b uint64) string {
	gib := float64(b) / (1024 * 1024 * 1024)
	if gib >= 1.0 {
		return fmt.Sprintf("%.1f GiB", gib)
	}
	mib := float64(b) / (1024 * 1024)
	return fmt.Sprintf("%.0f MiB", mib)
}

func shortGPUName(name string) string {
	name = strings.TrimPrefix(name, "NVIDIA ")
	parts := strings.Split(name, "-")
	if len(parts) >= 3 {
		return parts[0] + "-" + parts[len(parts)-1]
	}
	return name
}
