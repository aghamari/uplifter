package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/xuri/excelize/v2"
)

// CompareResult holds the comparison between two traces
type CompareResult struct {
	EagerName      string
	CompiledName   string
	EagerCycle     int
	CompiledCycle  int
	Matches        []KernelMatch
	TotalTime      float64 // Total time in compiled mode
}

// KernelMatch represents a matched pair of kernels between two traces
type KernelMatch struct {
	Index           int
	EagerKernels    []string // Kernel name(s) in eager mode (may be multiple if fused)
	CompiledKernel  string   // Kernel name in compiled mode
	CompiledDur     float64  // Duration in compiled mode (µs)
	MatchType       string   // "exact", "similar", "fused", "only_compiled"
	Signature       string   // Common signature used for matching
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

// matchKernelsBySignature matches kernels between eager and compiled traces
// Format follows the Excel: eager_kernel | compiled_kernel | duration
// Preserves COMPILED trace order, with fused eager kernels at the end
func matchKernelsBySignature(eagerResult, compiledResult *CycleResult) []KernelMatch {
	var matches []KernelMatch

	// Build maps for matching
	eagerByName := make(map[string]KernelStats)
	eagerBySig := make(map[string][]KernelStats)

	for _, k := range eagerResult.Kernels {
		eagerByName[k.Name] = k
		sig := getKernelSignature(k.Name)
		eagerBySig[sig] = append(eagerBySig[sig], k)
	}

	// Track which eager kernels have been matched
	matchedEager := make(map[string]bool)
	idx := 0

	// Iterate through COMPILED kernels in order (preserves execution order)
	for _, ck := range compiledResult.Kernels {
		sig := getKernelSignature(ck.Name)
		
		// Try exact match first
		if ek, exists := eagerByName[ck.Name]; exists && !matchedEager[ek.Name] {
			matches = append(matches, KernelMatch{
				Index:          idx,
				EagerKernels:   []string{ek.Name},
				CompiledKernel: ck.Name,
				CompiledDur:    ck.AvgDur,
				Signature:      sig,
				MatchType:      "exact",
			})
			matchedEager[ek.Name] = true
			idx++
			continue
		}

		// Try signature match (similar kernels)
		if eagerKernels, exists := eagerBySig[sig]; exists {
			var unmatched []string
			for _, ek := range eagerKernels {
				if !matchedEager[ek.Name] {
					unmatched = append(unmatched, ek.Name)
					matchedEager[ek.Name] = true
				}
			}
			if len(unmatched) > 0 {
				matches = append(matches, KernelMatch{
					Index:          idx,
					EagerKernels:   unmatched,
					CompiledKernel: ck.Name,
					CompiledDur:    ck.AvgDur,
					Signature:      sig,
					MatchType:      "similar",
				})
				idx++
				continue
			}
		}

		// No match - compiled-only (new fused kernel)
		matches = append(matches, KernelMatch{
			Index:          idx,
			EagerKernels:   []string{"(none)"},
			CompiledKernel: ck.Name,
			CompiledDur:    ck.AvgDur,
			Signature:      sig,
			MatchType:      "compiled_only",
		})
		idx++
	}

	// Append unmatched eager kernels at the end (fused/removed)
	for _, ek := range eagerResult.Kernels {
		if matchedEager[ek.Name] {
			continue
		}
		matches = append(matches, KernelMatch{
			Index:          idx,
			EagerKernels:   []string{ek.Name},
			CompiledKernel: ".",
			CompiledDur:    0,
			Signature:      getKernelSignature(ek.Name),
			MatchType:      "fused",
		})
		matchedEager[ek.Name] = true
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
			durStr = ""  // No duration for fused/removed kernels
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
				".",  // Already matched to compiled above
				"",
				"fused",
			}
			if err := writer.Write(extraRow); err != nil {
				return err
			}
		}
	}

	return nil
}

// WriteCompareXLSX writes the comparison result to an Excel file
func (r *CompareResult) WriteCompareXLSX(filename string) error {
	f := excelize.NewFile()
	defer f.Close()

	// Create sheet
	sheetName := "Comparison"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")

	// Define styles
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	exactStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#C6EFCE"}, Pattern: 1},
	})

	fusedStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFC7CE"}, Pattern: 1},
	})

	compiledOnlyStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFEB9C"}, Pattern: 1},
	})

	// Write headers
	headers := []string{"Eager Kernel", "Compiled Kernel", "Duration (µs)", "Match Type"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
		f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}

	// Set column widths
	f.SetColWidth(sheetName, "A", "A", 60)
	f.SetColWidth(sheetName, "B", "B", 60)
	f.SetColWidth(sheetName, "C", "C", 15)
	f.SetColWidth(sheetName, "D", "D", 15)

	// Write summary row
	f.SetCellValue(sheetName, "A2", fmt.Sprintf("Total (%d eager kernels)", r.EagerCycle))
	f.SetCellValue(sheetName, "B2", fmt.Sprintf("(%d compiled kernels)", r.CompiledCycle))
	f.SetCellValue(sheetName, "C2", r.TotalTime)

	// Write data rows
	row := 3
	for _, m := range r.Matches {
		eagerStr := "(none)"
		if len(m.EagerKernels) > 0 && m.EagerKernels[0] != "(none)" {
			eagerStr = m.EagerKernels[0]
		}

		compiledStr := m.CompiledKernel
		
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), eagerStr)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), compiledStr)
		if m.CompiledKernel != "." {
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), m.CompiledDur)
		}
		f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), m.MatchType)

		// Apply style based on match type
		rowRange := fmt.Sprintf("A%d:D%d", row, row)
		switch m.MatchType {
		case "exact":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), exactStyle)
		case "fused":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), fusedStyle)
		case "compiled_only":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), compiledOnlyStyle)
		}
		_ = rowRange // suppress unused warning

		row++

		// Additional rows for multiple eager kernels
		for i := 1; i < len(m.EagerKernels); i++ {
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), m.EagerKernels[i])
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), ".")
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), "fused")
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("D%d", row), fusedStyle)
			row++
		}
	}

	// Add auto-filter
	f.AutoFilter(sheetName, fmt.Sprintf("A1:D%d", row-1), nil)

	// Freeze first row
	f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	return f.SaveAs(filename)
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
	for i, col := range header {
		switch col {
		case "kernel_name":
			nameIdx = i
		case "avg_duration_us":
			avgDurIdx = i
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

		if len(record) <= avgDurIdx {
			continue
		}

		avgDur, err := strconv.ParseFloat(record[avgDurIdx], 64)
		if err != nil {
			continue // Skip invalid rows
		}

		kernels = append(kernels, KernelStats{
			Name:   record[nameIdx],
			AvgDur: avgDur,
		})
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

	for i := 0; i < minInt(10, len(entries)); i++ {
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
		if m.MatchType == "fused" {
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
		if m.MatchType == "compiled_only" {
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

