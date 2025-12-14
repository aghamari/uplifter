package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// CompareMode controls comparison algorithm: "align" or "match"
// align = position-based LCS alignment with auto rotation detection (default)
// match = signature-based matching (position-independent)
var CompareMode = "align"

// CompareResult holds the comparison between two traces
type CompareResult struct {
	EagerName     string
	CompiledName  string
	EagerCycle    int
	CompiledCycle int
	Matches       []KernelMatch
	TotalTime     float64 // Total time in compiled mode
}

// KernelMatch represents a matched pair of kernels between two traces
type KernelMatch struct {
	Index          int
	EagerKernels   []string // Kernel name(s) in eager mode (may be multiple if fused)
	CompiledKernel string   // Kernel name in compiled mode
	CompiledDur    float64  // Duration in compiled mode (µs)
	CompiledMin    float64  // Min duration in compiled mode
	CompiledMax    float64  // Max duration in compiled mode
	CompiledStdDev float64  // Std deviation in compiled mode
	EagerDur       float64  // Duration in eager/trace1 mode (µs) - may be 0 if no timing
	EagerMin       float64  // Min duration in eager mode
	EagerMax       float64  // Max duration in eager mode
	EagerStdDev    float64  // Std deviation in eager mode
	MatchType      string   // "exact", "similar", "removed", "new_only"
	Signature      string   // Common signature used for matching
}

// CompareTraces compares two trace files and produces a kernel-by-kernel comparison
// trace1 = eager mode (no timing), trace2 = compiled mode (has timing)
// Uses existing uplifter cycle detection, then matches the results
func CompareTraces(trace1Path, trace2Path string, fullParse bool) (*CompareResult, error) {
	startTotal := time.Now()

	// Analyze trace 1
	fmt.Fprintf(os.Stderr, "=== [1/2] Analyzing Trace 1: %s ===\n", filepath.Base(trace1Path))
	start1 := time.Now()
	result1, err := analyzeTrace(trace1Path, fullParse)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze trace 1: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Trace 1 done in %v\n", time.Since(start1))

	// Analyze trace 2
	fmt.Fprintf(os.Stderr, "\n=== [2/2] Analyzing Trace 2: %s ===\n", filepath.Base(trace2Path))
	start2 := time.Now()
	result2, err := analyzeTrace(trace2Path, fullParse)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze trace 2: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Trace 2 done in %v\n", time.Since(start2))

	fmt.Fprintf(os.Stderr, "\n=== Matching kernels by signature ===\n")
	fmt.Fprintf(os.Stderr, "Trace 1: %d kernels/cycle, Trace 2: %d kernels/cycle\n",
		len(result1.Kernels), len(result2.Kernels))

	// Match kernels between the two cycles using signatures
	startMatch := time.Now()
	matches := matchKernelsBySignature(result1, result2)
	fmt.Fprintf(os.Stderr, "Matching done in %v\n", time.Since(startMatch))

	// Calculate total time from trace 2 (the one with timing)
	var totalTime float64
	for _, m := range matches {
		totalTime += m.CompiledDur
	}

	fmt.Fprintf(os.Stderr, "Total analysis time: %v\n", time.Since(startTotal))

	return &CompareResult{
		EagerName:     filepath.Base(trace1Path),
		CompiledName:  filepath.Base(trace2Path),
		EagerCycle:    len(result1.Kernels),
		CompiledCycle: len(result2.Kernels),
		Matches:       matches,
		TotalTime:     totalTime,
	}, nil
}

// analyzeTrace runs the full cycle detection pipeline on a trace file
// Uses the SAME code as the main uplifter command
// Returns the sub-cycle (smallest repeating unit) with kernel statistics
func analyzeTrace(path string, fullParse bool) (*CycleResult, error) {
	// Step 1: Parse trace file
	fmt.Fprintf(os.Stderr, "  [Step 1] Parsing trace file...\n")
	parseStart := time.Now()

	var events []KernelEvent
	var err error

	if fullParse {
		events, err = ParseKernelEvents(path)
	} else {
		events, err = ParseWithEarlyStop(path, 50, 5000)
	}
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no kernel events found")
	}

	fmt.Fprintf(os.Stderr, "  [Step 1] Parsed %d kernel events in %v\n", len(events), time.Since(parseStart))

	// Step 2: Detect cycle
	fmt.Fprintf(os.Stderr, "  [Step 2] Detecting cycle...\n")
	cycleStart := time.Now()
	cycle, err := DetectCycleBySignature(events)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "  [Step 2] Cycle detected in %v\n", time.Since(cycleStart))

	// Step 3: Extract cycle statistics
	fmt.Fprintf(os.Stderr, "  [Step 3] Extracting cycle statistics...\n")
	extractStart := time.Now()
	result := ExtractCycle(events, cycle)
	fmt.Fprintf(os.Stderr, "  [Step 3] Extracted in %v: %d kernels, %d repetitions\n",
		time.Since(extractStart), result.CycleLength, cycle.NumCycles)

	return result, nil
}

// matchKernelsBySignature matches kernels using the selected comparison mode
// align = LCS position-based alignment (for eager vs compiled)
// match = signature-based matching (for compiled vs compiled)
func matchKernelsBySignature(eagerResult, compiledResult *CycleResult) []KernelMatch {
	if CompareMode == "align" {
		return matchByAlignment(eagerResult, compiledResult)
	}
	return matchBySignature(eagerResult, compiledResult)
}

// matchByAlignment uses LCS algorithm for position-based alignment
// Automatically finds the best rotation of baseline to maximize alignment
// Best for comparing cycles that may have different starting points
func matchByAlignment(eagerResult, compiledResult *CycleResult) []KernelMatch {
	eager := eagerResult.Kernels
	compiled := compiledResult.Kernels

	// Build signature arrays
	eagerSigs := make([]string, len(eager))
	compiledSigs := make([]string, len(compiled))
	for i, k := range eager {
		eagerSigs[i] = getKernelSignature(k.Name)
	}
	for i, k := range compiled {
		compiledSigs[i] = getKernelSignature(k.Name)
	}

	// Find best rotation of baseline to maximize LCS
	bestRotation := 0
	bestLCS := computeLCS(eagerSigs, compiledSigs)

	// Only try rotations if sequences are same length (cycle comparison)
	if len(eager) == len(compiled) && len(eager) > 0 {
		for rot := 1; rot < len(eager); rot++ {
			rotatedSigs := rotateSlice(eagerSigs, rot)
			lcs := computeLCS(rotatedSigs, compiledSigs)
			if lcs > bestLCS {
				bestLCS = lcs
				bestRotation = rot
			}
		}

		if bestRotation > 0 {
			fmt.Fprintf(os.Stderr, "Detected cycle rotation: baseline rotated by %d positions for best alignment\n", bestRotation)
			// Rotate both signatures and kernels
			eagerSigs = rotateSlice(eagerSigs, bestRotation)
			eager = rotateKernels(eager, bestRotation)
		}
	}

	// Compute LCS matrix with (possibly rotated) baseline
	m, n := len(eager), len(compiled)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if eagerSigs[i-1] == compiledSigs[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] > lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	// Backtrack to find alignment
	var alignedMatches []KernelMatch
	i, j := m, n

	for i > 0 || j > 0 {
		if i > 0 && j > 0 && eagerSigs[i-1] == compiledSigs[j-1] {
			ek := eager[i-1]
			ck := compiled[j-1]
			matchType := "similar"
			if ek.Name == ck.Name {
				matchType = "exact"
			}
			alignedMatches = append(alignedMatches, KernelMatch{
				EagerKernels:   []string{ek.Name},
				CompiledKernel: ck.Name,
				CompiledDur:    ck.AvgDur,
				CompiledMin:    ck.MinDur,
				CompiledMax:    ck.MaxDur,
				CompiledStdDev: ck.StdDev,
				EagerDur:       ek.AvgDur,
				EagerMin:       ek.MinDur,
				EagerMax:       ek.MaxDur,
				EagerStdDev:    ek.StdDev,
				Signature:      eagerSigs[i-1],
				MatchType:      matchType,
			})
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			ck := compiled[j-1]
			alignedMatches = append(alignedMatches, KernelMatch{
				EagerKernels:   []string{""},
				CompiledKernel: ck.Name,
				CompiledDur:    ck.AvgDur,
				CompiledMin:    ck.MinDur,
				CompiledMax:    ck.MaxDur,
				CompiledStdDev: ck.StdDev,
				Signature:      compiledSigs[j-1],
				MatchType:      "new_only",
			})
			j--
		} else {
			ek := eager[i-1]
			alignedMatches = append(alignedMatches, KernelMatch{
				EagerKernels:   []string{ek.Name},
				CompiledKernel: ".",
				EagerDur:       ek.AvgDur,
				EagerMin:       ek.MinDur,
				EagerMax:       ek.MaxDur,
				EagerStdDev:    ek.StdDev,
				Signature:      eagerSigs[i-1],
				MatchType:      "removed",
			})
			i--
		}
	}

	// Reverse to get correct order
	var matches []KernelMatch
	for k := len(alignedMatches) - 1; k >= 0; k-- {
		match := alignedMatches[k]
		match.Index = len(matches)
		matches = append(matches, match)
	}
	return matches
}

// computeLCS returns the length of the longest common subsequence
func computeLCS(a, b []string) int {
	m, n := len(a), len(b)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] > lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}
	return lcs[m][n]
}

// rotateSlice rotates a string slice by n positions
func rotateSlice(s []string, n int) []string {
	if len(s) == 0 {
		return s
	}
	n = n % len(s)
	result := make([]string, len(s))
	for i := range s {
		result[i] = s[(i+n)%len(s)]
	}
	return result
}

// rotateKernels rotates a KernelStats slice by n positions
func rotateKernels(k []KernelStats, n int) []KernelStats {
	if len(k) == 0 {
		return k
	}
	n = n % len(k)
	result := make([]KernelStats, len(k))
	for i := range k {
		result[i] = k[(i+n)%len(k)]
	}
	return result
}

// matchBySignature uses greedy signature matching
// Best for compiled vs compiled where kernels may move positions
func matchBySignature(eagerResult, compiledResult *CycleResult) []KernelMatch {
	eager := eagerResult.Kernels
	compiled := compiledResult.Kernels

	type eagerEntry struct {
		idx    int
		kernel KernelStats
	}
	eagerBySig := make(map[string][]eagerEntry)
	eagerByName := make(map[string][]eagerEntry)

	for i, k := range eager {
		entry := eagerEntry{idx: i, kernel: k}
		sig := getKernelSignature(k.Name)
		eagerBySig[sig] = append(eagerBySig[sig], entry)
		eagerByName[k.Name] = append(eagerByName[k.Name], entry)
	}

	matchedEagerIdx := make(map[int]bool)
	var matches []KernelMatch
	idx := 0

	for _, ck := range compiled {
		sig := getKernelSignature(ck.Name)
		var matched *eagerEntry
		matchType := ""

		if entries, exists := eagerByName[ck.Name]; exists {
			for i := range entries {
				if !matchedEagerIdx[entries[i].idx] {
					matched = &entries[i]
					matchType = "exact"
					break
				}
			}
		}

		if matched == nil {
			if entries, exists := eagerBySig[sig]; exists {
				for i := range entries {
					if !matchedEagerIdx[entries[i].idx] {
						matched = &entries[i]
						matchType = "similar"
						break
					}
				}
			}
		}

		if matched != nil {
			matchedEagerIdx[matched.idx] = true
			matches = append(matches, KernelMatch{
				Index:          idx,
				EagerKernels:   []string{matched.kernel.Name},
				CompiledKernel: ck.Name,
				CompiledDur:    ck.AvgDur,
				CompiledMin:    ck.MinDur,
				CompiledMax:    ck.MaxDur,
				CompiledStdDev: ck.StdDev,
				EagerDur:       matched.kernel.AvgDur,
				EagerMin:       matched.kernel.MinDur,
				EagerMax:       matched.kernel.MaxDur,
				EagerStdDev:    matched.kernel.StdDev,
				Signature:      sig,
				MatchType:      matchType,
			})
		} else {
			matches = append(matches, KernelMatch{
				Index:          idx,
				EagerKernels:   []string{""},
				CompiledKernel: ck.Name,
				CompiledDur:    ck.AvgDur,
				CompiledMin:    ck.MinDur,
				CompiledMax:    ck.MaxDur,
				CompiledStdDev: ck.StdDev,
				Signature:      sig,
				MatchType:      "new_only",
			})
		}
		idx++
	}

	// Append unmatched eager kernels (removed)
	for i, ek := range eager {
		if matchedEagerIdx[i] {
			continue
		}
		matches = append(matches, KernelMatch{
			Index:          idx,
			EagerKernels:   []string{ek.Name},
			CompiledKernel: ".",
			EagerDur:       ek.AvgDur,
			EagerMin:       ek.MinDur,
			EagerMax:       ek.MaxDur,
			EagerStdDev:    ek.StdDev,
			Signature:      getKernelSignature(ek.Name),
			MatchType:      "removed",
		})
		idx++
	}

	return matches
}

// WriteCompareCSV writes the comparison result to a CSV file
// Format matches the Excel: eager_kernel | compiled_kernel | duration_us
func (r *CompareResult) WriteCompareCSV(w io.Writer) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header matching Excel format
	headers := []string{
		"eager_kernel",
		"compiled_kernel",
		"duration_us",
		"match_type",
	}
	if err := writer.Write(headers); err != nil {
		return err
	}

	// Write summary row
	summaryRow := []string{
		fmt.Sprintf("Total (%d eager kernels)", r.EagerCycle),
		fmt.Sprintf("(%d compiled kernels)", r.CompiledCycle),
		fmt.Sprintf("%.3f", r.TotalTime),
		"",
	}
	if err := writer.Write(summaryRow); err != nil {
		return err
	}

	// Write kernel rows - one row per match
	for _, m := range r.Matches {
		eagerStr := "(none)"
		if len(m.EagerKernels) > 0 && m.EagerKernels[0] != "(none)" {
			eagerStr = m.EagerKernels[0]
		}

		compiledStr := m.CompiledKernel
		durStr := fmt.Sprintf("%.3f", m.CompiledDur)
		if m.CompiledKernel == "." {
			durStr = "" // No duration for fused/removed kernels
		}

		row := []string{
			eagerStr,
			compiledStr,
			durStr,
			m.MatchType,
		}
		if err := writer.Write(row); err != nil {
			return err
		}

		// If multiple eager kernels matched to one compiled, show them on additional rows
		for i := 1; i < len(m.EagerKernels); i++ {
			extraRow := []string{
				m.EagerKernels[i],
				".", // Already matched to compiled above
				"",
				"removed",
			}
			if err := writer.Write(extraRow); err != nil {
				return err
			}
		}
	}

	return nil
}

// CompareFromCSV compares two pre-extracted CSV files (much faster than raw traces)
// csv1 = eager mode, csv2 = compiled mode
func CompareFromCSV(csv1Path, csv2Path string) (*CompareResult, error) {
	startTotal := time.Now()

	fmt.Fprintf(os.Stderr, "=== Reading eager CSV: %s ===\n", filepath.Base(csv1Path))
	eagerKernels, err := readKernelsFromCSV(csv1Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read eager CSV: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Read %d kernels\n", len(eagerKernels))

	fmt.Fprintf(os.Stderr, "=== Reading compiled CSV: %s ===\n", filepath.Base(csv2Path))
	compiledKernels, err := readKernelsFromCSV(csv2Path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compiled CSV: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Read %d kernels\n", len(compiledKernels))

	// Create CycleResult structures for matching
	eagerResult := &CycleResult{Kernels: eagerKernels, CycleLength: len(eagerKernels)}
	compiledResult := &CycleResult{Kernels: compiledKernels, CycleLength: len(compiledKernels)}

	fmt.Fprintf(os.Stderr, "\n=== Matching kernels ===\n")
	matches := matchKernelsBySignature(eagerResult, compiledResult)

	var totalTime float64
	for _, m := range matches {
		totalTime += m.CompiledDur
	}

	fmt.Fprintf(os.Stderr, "Matching done in %v\n", time.Since(startTotal))

	return &CompareResult{
		EagerName:     filepath.Base(csv1Path),
		CompiledName:  filepath.Base(csv2Path),
		EagerCycle:    len(eagerKernels),
		CompiledCycle: len(compiledKernels),
		Matches:       matches,
		TotalTime:     totalTime,
	}, nil
}

// readKernelsFromCSV reads kernel stats from a CSV file produced by uplifter
func readKernelsFromCSV(path string) ([]KernelStats, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read header
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	// Find column indices
	nameIdx := -1
	avgDurIdx := -1
	minDurIdx := -1
	maxDurIdx := -1
	stdDevIdx := -1
	for i, col := range header {
		switch col {
		case "kernel_name":
			nameIdx = i
		case "avg_duration_us":
			avgDurIdx = i
		case "min_duration_us":
			minDurIdx = i
		case "max_duration_us":
			maxDurIdx = i
		case "stddev_us":
			stdDevIdx = i
		}
	}

	if nameIdx == -1 || avgDurIdx == -1 {
		return nil, fmt.Errorf("CSV missing required columns (kernel_name, avg_duration_us)")
	}

	var kernels []KernelStats
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV row: %w", err)
		}

		// Validate both required column indices are within bounds
		if len(record) <= avgDurIdx || len(record) <= nameIdx {
			continue
		}

		avgDur, err := strconv.ParseFloat(record[avgDurIdx], 64)
		if err != nil {
			continue // Skip invalid rows
		}

		k := KernelStats{
			Name:   record[nameIdx],
			AvgDur: avgDur,
		}

		// Parse optional stats if columns exist
		if minDurIdx >= 0 && minDurIdx < len(record) {
			if v, err := strconv.ParseFloat(record[minDurIdx], 64); err == nil {
				k.MinDur = v
			}
		}
		if maxDurIdx >= 0 && maxDurIdx < len(record) {
			if v, err := strconv.ParseFloat(record[maxDurIdx], 64); err == nil {
				k.MaxDur = v
			}
		}
		if stdDevIdx >= 0 && stdDevIdx < len(record) {
			if v, err := strconv.ParseFloat(record[stdDevIdx], 64); err == nil {
				k.StdDev = v
			}
		}

		kernels = append(kernels, k)
	}

	return kernels, nil
}

// WriteSummary writes a human-readable comparison summary
func (r *CompareResult) WriteSummary(w io.Writer) {
	fmt.Fprintf(w, "\n=== Trace Comparison Summary ===\n")
	fmt.Fprintf(w, "Eager:    %s (%d kernels/cycle)\n", r.EagerName, r.EagerCycle)
	fmt.Fprintf(w, "Compiled: %s (%d kernels/cycle)\n", r.CompiledName, r.CompiledCycle)
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "Total Compiled Cycle Time: %.2f µs (%.4f ms)\n", r.TotalTime, r.TotalTime/1000)
	fmt.Fprintf(w, "\n")

	// Count match types
	typeCounts := make(map[string]int)
	for _, m := range r.Matches {
		typeCounts[m.MatchType]++
	}

	fmt.Fprintf(w, "Match Types:\n")
	for matchType, count := range typeCounts {
		fmt.Fprintf(w, "  %s: %d\n", matchType, count)
	}
	fmt.Fprintf(w, "\n")

	// Top kernels by duration
	fmt.Fprintf(w, "=== Top 10 Kernels by Duration (Compiled) ===\n")
	type kernelEntry struct {
		compiled  string
		eager     []string
		dur       float64
		matchType string
	}
	var entries []kernelEntry
	for _, m := range r.Matches {
		if m.CompiledDur > 0 {
			entries = append(entries, kernelEntry{
				compiled:  m.CompiledKernel,
				eager:     m.EagerKernels,
				dur:       m.CompiledDur,
				matchType: m.MatchType,
			})
		}
	}

	// Sort by duration descending
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].dur > entries[i].dur {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	for i := 0; i < min(10, len(entries)); i++ {
		e := entries[i]
		pct := 0.0
		if r.TotalTime > 0 {
			pct = (e.dur / r.TotalTime) * 100
		}
		fmt.Fprintf(w, "%2d. %.2f µs (%.1f%%) - %s\n", i+1, e.dur, pct, e.matchType)
		fmt.Fprintf(w, "    Compiled: %s\n", truncateString(e.compiled, 65))
		if len(e.eager) > 0 && e.eager[0] != "(none)" {
			fmt.Fprintf(w, "    Eager:    %s\n", truncateString(e.eager[0], 65))
		}
	}

	// Fused kernels (eager kernels that were removed in compiled)
	fmt.Fprintf(w, "\n=== Fused/Removed Eager Kernels (no compiled equivalent) ===\n")
	fusedCount := 0
	for _, m := range r.Matches {
		if m.MatchType == "removed" {
			fusedCount++
			for _, ek := range m.EagerKernels {
				fmt.Fprintf(w, "  - %s\n", truncateString(ek, 75))
			}
		}
	}
	if fusedCount == 0 {
		fmt.Fprintf(w, "  (none)\n")
	}

	// Compiled-only kernels (new fused kernels)
	fmt.Fprintf(w, "\n=== Compiled-Only Kernels (new fused kernels) ===\n")
	compiledOnlyCount := 0
	for _, m := range r.Matches {
		if m.MatchType == "new_only" {
			compiledOnlyCount++
			pct := 0.0
			if r.TotalTime > 0 {
				pct = (m.CompiledDur / r.TotalTime) * 100
			}
			fmt.Fprintf(w, "  %.2f µs (%.1f%%) %s\n", m.CompiledDur, pct, truncateString(m.CompiledKernel, 60))
		}
	}
	if compiledOnlyCount == 0 {
		fmt.Fprintf(w, "  (none)\n")
	}
}
