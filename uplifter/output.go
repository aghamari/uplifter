package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
)

// CycleResult contains the extracted cycle data with statistics
type CycleResult struct {
	CycleLength     int            `json:"cycle_length"`
	NumCycles       int            `json:"num_cycles"`
	TotalCycleTime  float64        `json:"total_cycle_time_us"`
	AvgCycleTime    float64        `json:"avg_cycle_time_us"`
	Kernels         []KernelStats  `json:"kernels"`
	KernelsByName   map[string]int `json:"-"` // For quick lookup
}

// ExtractCycle extracts one representative cycle from the events using the detected cycle info
func ExtractCycle(events []KernelEvent, cycleInfo *CycleInfo) *CycleResult {
	result := &CycleResult{
		CycleLength:   cycleInfo.CycleLength,
		NumCycles:     cycleInfo.NumCycles,
		Kernels:       make([]KernelStats, 0, cycleInfo.CycleLength),
		KernelsByName: make(map[string]int),
	}

	// Aggregate statistics across all detected cycles
	kernelStats := make(map[int]*KernelStats) // Position -> Stats

	for cycleIdx, cycleStart := range cycleInfo.CycleIndices {
		cycleTime := 0.0
		for i := 0; i < cycleInfo.CycleLength && cycleStart+i < len(events); i++ {
			event := events[cycleStart+i]
			cycleTime += event.Duration

			if _, exists := kernelStats[i]; !exists {
				kernelStats[i] = &KernelStats{
					Name:         event.Name,
					IndexInCycle: i,
					MinDur:       event.Duration,
					MaxDur:       event.Duration,
					Durations:    make([]float64, 0, cycleInfo.NumCycles),
				}
			}

			stats := kernelStats[i]
			stats.TotalDur += event.Duration
			stats.Count++
			stats.Durations = append(stats.Durations, event.Duration)
			if event.Duration < stats.MinDur {
				stats.MinDur = event.Duration
			}
			if event.Duration > stats.MaxDur {
				stats.MaxDur = event.Duration
			}
		}

		result.TotalCycleTime += cycleTime
		_ = cycleIdx // Used for potential per-cycle tracking
	}

	result.AvgCycleTime = result.TotalCycleTime / float64(cycleInfo.NumCycles)

	// Convert map to sorted slice and compute stddev
	positions := make([]int, 0, len(kernelStats))
	for pos := range kernelStats {
		positions = append(positions, pos)
	}
	sort.Ints(positions)

	for _, pos := range positions {
		stats := kernelStats[pos]
		stats.AvgDur = stats.TotalDur / float64(stats.Count)
		// Compute standard deviation
		if len(stats.Durations) > 1 {
			var sumSquares float64
			for _, d := range stats.Durations {
				diff := d - stats.AvgDur
				sumSquares += diff * diff
			}
			stats.StdDev = math.Sqrt(sumSquares / float64(len(stats.Durations)))
		}
		// Clear durations to save memory (we have stddev now)
		stats.Durations = nil
		result.Kernels = append(result.Kernels, *stats)
		result.KernelsByName[stats.Name] = pos
	}

	return result
}

// WriteCSV writes the cycle result to CSV format
func (r *CycleResult) WriteCSV(w io.Writer) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write cycle metadata as comment rows
	metaRows := [][]string{
		{"# Cycle Statistics"},
		{"# Iterations", strconv.Itoa(r.NumCycles)},
		{"# Kernels per cycle", strconv.Itoa(r.CycleLength)},
		{"# Avg cycle time (us)", fmt.Sprintf("%.3f", r.AvgCycleTime)},
		{"# Total time (us)", fmt.Sprintf("%.3f", r.TotalCycleTime)},
		{}, // Empty row before data
	}
	for _, row := range metaRows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	// Write header
	headers := []string{
		"index",
		"kernel_name",
		"avg_duration_us",
		"min_duration_us",
		"max_duration_us",
		"stddev_us",
		"count",
		"pct_of_cycle",
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	// Write kernel rows
	for _, k := range r.Kernels {
		pctOfCycle := (k.AvgDur / r.AvgCycleTime) * 100
		row := []string{
			strconv.Itoa(k.IndexInCycle),
			k.Name,
			fmt.Sprintf("%.3f", k.AvgDur),
			fmt.Sprintf("%.3f", k.MinDur),
			fmt.Sprintf("%.3f", k.MaxDur),
			fmt.Sprintf("%.3f", k.StdDev),
			strconv.Itoa(k.Count),
			fmt.Sprintf("%.4f", pctOfCycle),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// WriteJSON writes the cycle result to JSON format
func (r *CycleResult) WriteJSON(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r)
}

// WriteSummary writes a human-readable summary
func (r *CycleResult) WriteSummary(w io.Writer) {
	fmt.Fprintf(w, "\n=== Cycle Analysis Summary ===\n")
	fmt.Fprintf(w, "Cycle Length: %d kernels\n", r.CycleLength)
	fmt.Fprintf(w, "Number of Cycles: %d\n", r.NumCycles)
	fmt.Fprintf(w, "Average Cycle Time: %.2f µs (%.4f ms)\n", r.AvgCycleTime, r.AvgCycleTime/1000)
	fmt.Fprintf(w, "Total Measured Time: %.2f µs (%.4f ms)\n", r.TotalCycleTime, r.TotalCycleTime/1000)
	fmt.Fprintf(w, "\n")

	// Top 10 kernels by duration
	fmt.Fprintf(w, "=== Top 10 Kernels by Average Duration ===\n")
	sorted := make([]KernelStats, len(r.Kernels))
	copy(sorted, r.Kernels)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].AvgDur > sorted[j].AvgDur
	})

	for i := 0; i < min(10, len(sorted)); i++ {
		k := sorted[i]
		pct := (k.AvgDur / r.AvgCycleTime) * 100
		fmt.Fprintf(w, "%2d. [%4d] %s\n", i+1, k.IndexInCycle, truncateString(k.Name, 80))
		fmt.Fprintf(w, "          Avg: %.2f µs | Min: %.2f | Max: %.2f | StdDev: %.2f  (%.2f%% of cycle)\n",
			k.AvgDur, k.MinDur, k.MaxDur, k.StdDev, pct)
	}
	fmt.Fprintf(w, "\n")

	// Kernel type distribution
	fmt.Fprintf(w, "=== Kernel Type Distribution ===\n")
	typeCounts := make(map[string]struct {
		count int
		dur   float64
	})

	for _, k := range r.Kernels {
		kernelType := categorizeKernel(k.Name)
		entry := typeCounts[kernelType]
		entry.count++
		entry.dur += k.AvgDur
		typeCounts[kernelType] = entry
	}

	type typeInfo struct {
		name  string
		count int
		dur   float64
	}
	var types []typeInfo
	for name, info := range typeCounts {
		types = append(types, typeInfo{name, info.count, info.dur})
	}
	sort.Slice(types, func(i, j int) bool {
		return types[i].dur > types[j].dur
	})

	for _, t := range types {
		pct := (t.dur / r.AvgCycleTime) * 100
		fmt.Fprintf(w, "  %-20s: %4d kernels, %.2f µs (%.1f%%)\n", t.name, t.count, t.dur, pct)
	}
}

// categorizeKernel attempts to categorize a kernel by its name
func categorizeKernel(name string) string {
	// Check for common patterns
	patterns := []struct {
		substr   string
		category string
	}{
		{"Cijk_", "GEMM/BLAS"},
		{"triton_", "Triton"},
		{"attention", "Attention"},
		{"fmha", "FlashAttention"},
		{"paged_attention", "PagedAttention"},
		{"elementwise", "Elementwise"},
		{"reduce", "Reduce"},
		{"norm", "Normalization"},
		{"softmax", "Softmax"},
		{"embedding", "Embedding"},
		{"copy", "Memory"},
		{"fill", "Memory"},
		{"reshape", "Memory"},
		{"transpose", "Memory"},
		{"rocprim", "ROCm Primitives"},
		{"ck_tile", "Composable Kernel"},
	}

	for _, p := range patterns {
		if containsIgnoreCase(name, p.substr) {
			return p.category
		}
	}

	return "Other"
}

func containsIgnoreCase(s, substr string) bool {
	// Simple case-insensitive contains
	sLower := toLower(s)
	substrLower := toLower(substr)
	return contains(sLower, substrLower)
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// WriteToFile writes the result to a file based on extension
func (r *CycleResult) WriteToFile(filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	if len(filename) > 5 && filename[len(filename)-5:] == ".json" {
		return r.WriteJSON(file)
	} else if len(filename) > 4 && filename[len(filename)-4:] == ".csv" {
		return r.WriteCSV(file)
	} else {
		// Default to summary
		r.WriteSummary(file)
		return nil
	}
}

