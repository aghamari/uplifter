package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func main() {
	// Check for subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "compare-csv":
			runCompareCSV(os.Args[2:])
			return
		case "compare-all":
			runCompareAll(os.Args[2:])
			return
		case "test-kmer":
			if len(os.Args) < 3 {
				fmt.Fprintf(os.Stderr, "Usage: uplifter test-kmer <trace.json.gz>\n")
				os.Exit(1)
			}
			RunKmerTest(os.Args[2])
			return
		case "kmer":
			runKmerDetection(os.Args[2:])
			return
		}
	}

	// Default: cycle detection mode
	runCycleDetection()
}

func runCompareCSV(args []string) {
	compareFlags := flag.NewFlagSet("compare-csv", flag.ExitOnError)
	csv1 := compareFlags.String("baseline", "", "Path to baseline CSV")
	csv2 := compareFlags.String("new", "", "Path to new/optimized CSV")
	outputFile := compareFlags.String("output", "", "Output file path (.csv or .xlsx)")
	showSummary := compareFlags.Bool("summary", true, "Print summary to stderr")
	mode := compareFlags.String("mode", "align", "Comparison mode: 'align' (default, position-based with rotation) or 'match' (signature-based, position-independent)")

	compareFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uplifter Compare - Compare kernel cycles between two traces\n\n")
		fmt.Fprintf(os.Stderr, "Usage: uplifter compare-csv -baseline <baseline.csv> -new <new.csv> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		compareFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nModes:\n")
		fmt.Fprintf(os.Stderr, "  align - Position-based alignment with auto rotation detection (default)\n")
		fmt.Fprintf(os.Stderr, "          Shows insertions/deletions in execution order\n")
		fmt.Fprintf(os.Stderr, "  match - Signature-based matching (position-independent)\n")
		fmt.Fprintf(os.Stderr, "          Finds best matches regardless of position\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # Compare two traces (align mode is default):\n")
		fmt.Fprintf(os.Stderr, "  uplifter compare-csv -baseline baseline.csv -new optimized.csv -output compare.xlsx\n")
		fmt.Fprintf(os.Stderr, "\n  # Use match mode for heavily reordered traces:\n")
		fmt.Fprintf(os.Stderr, "  uplifter compare-csv -baseline a.csv -new b.csv -mode match -output compare.xlsx\n")
	}

	compareFlags.Parse(args)

	if *csv1 == "" || *csv2 == "" {
		fmt.Fprintf(os.Stderr, "Error: -baseline and -new are required\n\n")
		compareFlags.Usage()
		os.Exit(1)
	}

	startTime := time.Now()

	// Set global comparison mode
	CompareMode = *mode

	result, err := CompareFromCSV(*csv1, *csv2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error comparing CSVs: %v\n", err)
		os.Exit(1)
	}

	if *showSummary {
		result.WriteSummary(os.Stderr)
	}

	if *outputFile != "" {
		if strings.HasSuffix(*outputFile, ".xlsx") {
			if err := result.WriteCompareXLSX(*outputFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing XLSX: %v\n", err)
				os.Exit(1)
			}
		} else {
			file, err := os.Create(*outputFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
				os.Exit(1)
			}
			defer file.Close()

			if err := result.WriteCompareCSV(file); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
				os.Exit(1)
			}
		}
		fmt.Fprintf(os.Stderr, "\nResults written to: %s\n", *outputFile)
	} else {
		result.WriteCompareCSV(os.Stdout)
	}

	fmt.Fprintf(os.Stderr, "Total execution time: %v\n", time.Since(startTime))
}

func runCycleDetection() {
	// Define command line flags
	inputFile := flag.String("input", "", "Path to Perfetto JSON trace file (required)")
	outputBase := flag.String("output", "", "Output base path for CSV files")
	showSummary := flag.Bool("summary", true, "Print summary to stderr")
	mode := flag.String("mode", "all", "Detection mode: 'all' (default, all cycles) or 'llm' (prefill/decode)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uplifter - Perfetto trace cycle detector\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s -input <trace.json.gz> -output <basename> [-mode all|llm]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Modes:\n")
		fmt.Fprintf(os.Stderr, "  all - Output all detected cycle patterns (default)\n")
		fmt.Fprintf(os.Stderr, "        Creates: <basename>_cycle_1.csv, <basename>_cycle_2.csv, ...\n")
		fmt.Fprintf(os.Stderr, "  llm - Detect prefill and decode phases\n")
		fmt.Fprintf(os.Stderr, "        Creates: <basename>_prefill.csv, <basename>_decode.csv\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -input trace.json.gz -output analysis\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -input trace.json.gz -output analysis -mode llm\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s compare-csv -baseline cycle_1.csv -new cycle_2.csv -output compare.xlsx\n", os.Args[0])
	}

	flag.Parse()

	// Validate required arguments
	if *inputFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -input is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Check if input file exists
	if _, err := os.Stat(*inputFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: input file does not exist: %s\n", *inputFile)
		os.Exit(1)
	}

	startTime := time.Now()

	// Step 1: Parse kernel events from the trace (always full parse)
	fmt.Fprintf(os.Stderr, "Parsing trace file: %s\n", *inputFile)
	events, err := ParseKernelEvents(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing trace: %v\n", err)
		os.Exit(1)
	}

	parseTime := time.Since(startTime)
	fmt.Fprintf(os.Stderr, "Parsed %d kernel events in %v\n", len(events), parseTime)

	if len(events) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no kernel events found in trace\n")
		os.Exit(1)
	}

	// Step 2: Detect ALL cycle patterns
	fmt.Fprintf(os.Stderr, "\n=== Detecting cycle patterns ===\n")
	patterns := findAllCyclePatterns(events)

	if len(patterns) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no cycle patterns found\n")
		os.Exit(1)
	}

	// Display all patterns
	fmt.Fprintf(os.Stderr, "Found %d distinct patterns:\n", len(patterns))
	for i, p := range patterns {
		fmt.Fprintf(os.Stderr, "  %d. length=%d, reps=%d, center=%.1f%%, sig=%s\n",
			i+1, p.Info.CycleLength, p.Info.NumCycles,
			p.CenterPos/float64(len(events))*100,
			truncateString(p.Signature, 50))
	}

	detectTime := time.Since(startTime) - parseTime
	fmt.Fprintf(os.Stderr, "\nCycle detection completed in %v\n", detectTime)

	// Step 3: Output based on mode
	if *mode == "all" {
		outputAllPatterns(events, patterns, *outputBase, *showSummary)
	} else {
		// LLM mode: classify into prefill and decode
		prefillPattern, decodePattern := classifyPatterns(patterns, len(events))
		outputResults(events, prefillPattern, decodePattern, *outputBase, *showSummary)
	}

	totalTime := time.Since(startTime)
	fmt.Fprintf(os.Stderr, "\nTotal execution time: %v\n", totalTime)
}

// classifyPatterns selects prefill and decode patterns from all detected patterns
// Uses a combination of temporal position AND pattern significance (total events covered)
func classifyPatterns(patterns []CyclePattern, totalEvents int) (*CyclePattern, *CyclePattern) {
	if len(patterns) == 0 {
		return nil, nil
	}

	// Calculate significance for each pattern (total events covered)
	type scoredPattern struct {
		pattern      *CyclePattern
		significance int // reps * length = total kernel events
		centerPct    float64
	}

	var scored []scoredPattern
	for i := range patterns {
		p := &patterns[i]
		sig := p.Info.NumCycles * p.Info.CycleLength
		centerPct := p.CenterPos / float64(totalEvents) * 100
		scored = append(scored, scoredPattern{p, sig, centerPct})
	}

	// Filter to significant patterns (cover at least 1% of total events)
	minSignificance := totalEvents / 100
	var significant []scoredPattern
	for _, s := range scored {
		if s.significance >= minSignificance {
			significant = append(significant, s)
		}
	}

	// If no significant patterns, use all
	if len(significant) == 0 {
		significant = scored
	}

	fmt.Fprintf(os.Stderr, "\nSignificant patterns (>1%% of trace):\n")
	for _, s := range significant {
		fmt.Fprintf(os.Stderr, "  - length=%d, reps=%d, events=%d, center=%.1f%%\n",
			s.pattern.Info.CycleLength, s.pattern.Info.NumCycles,
			s.significance, s.centerPct)
	}

	// Find prefill: significant pattern with earliest center
	var prefill *CyclePattern
	minCenter := float64(101) // > 100%
	for _, s := range significant {
		if s.centerPct < minCenter {
			minCenter = s.centerPct
			prefill = s.pattern
		}
	}

	// Find decode: significant pattern with latest center (different from prefill)
	var decode *CyclePattern
	maxCenter := float64(-1)
	for _, s := range significant {
		// Skip if same signature as prefill
		if prefill != nil && s.pattern.Signature == prefill.Signature {
			continue
		}
		if s.centerPct > maxCenter {
			maxCenter = s.centerPct
			decode = s.pattern
		}
	}

	// If we only found one pattern, use it for both
	if prefill == nil && decode != nil {
		prefill = decode
	}
	if decode == nil && prefill != nil {
		decode = prefill
	}

	if prefill != nil {
		fmt.Fprintf(os.Stderr, "\nPREFILL: length=%d, reps=%d, center=%.1f%%\n",
			prefill.Info.CycleLength, prefill.Info.NumCycles,
			prefill.CenterPos/float64(totalEvents)*100)
	}
	if decode != nil {
		fmt.Fprintf(os.Stderr, "DECODE:  length=%d, reps=%d, center=%.1f%%\n",
			decode.Info.CycleLength, decode.Info.NumCycles,
			decode.CenterPos/float64(totalEvents)*100)
	}

	return prefill, decode
}

func outputResults(events []KernelEvent, prefill, decode *CyclePattern, outputBase string, showSummary bool) {
	// Extract and write prefill
	if prefill != nil {
		prefillResult := ExtractCycle(events, prefill.Info)
		if showSummary {
			fmt.Fprintf(os.Stderr, "\n=== PREFILL Cycle Summary ===\n")
			fmt.Fprintf(os.Stderr, "Cycle Length: %d kernels\n", prefillResult.CycleLength)
			fmt.Fprintf(os.Stderr, "Number of Cycles: %d\n", prefillResult.NumCycles)
			fmt.Fprintf(os.Stderr, "Average Cycle Time: %.2f µs\n", prefillResult.AvgCycleTime)
		}
		if outputBase != "" {
			prefillFile := outputBase + "_prefill.csv"
			if err := prefillResult.WriteToFile(prefillFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing prefill CSV: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Prefill results written to: %s\n", prefillFile)
			}
		}
	}

	// Extract and write decode
	if decode != nil {
		decodeResult := ExtractCycle(events, decode.Info)
		if showSummary {
			fmt.Fprintf(os.Stderr, "\n=== DECODE Cycle Summary ===\n")
			fmt.Fprintf(os.Stderr, "Cycle Length: %d kernels\n", decodeResult.CycleLength)
			fmt.Fprintf(os.Stderr, "Number of Cycles: %d\n", decodeResult.NumCycles)
			fmt.Fprintf(os.Stderr, "Average Cycle Time: %.2f µs\n", decodeResult.AvgCycleTime)
		}
		if outputBase != "" {
			decodeFile := outputBase + "_decode.csv"
			if err := decodeResult.WriteToFile(decodeFile); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing decode CSV: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "Decode results written to: %s\n", decodeFile)
			}
		}
	}

	// If no output specified, write decode to stdout
	if outputBase == "" && decode != nil {
		decodeResult := ExtractCycle(events, decode.Info)
		decodeResult.WriteCSV(os.Stdout)
	}
}

// outputAllPatterns outputs all detected cycle patterns as separate CSV files
func outputAllPatterns(events []KernelEvent, patterns []CyclePattern, outputBase string, showSummary bool) {
	if len(patterns) == 0 {
		fmt.Fprintf(os.Stderr, "No patterns to output\n")
		return
	}

	// Sort patterns by center position for consistent ordering
	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].CenterPos < patterns[j].CenterPos
	})

	fmt.Fprintf(os.Stderr, "\n=== Outputting %d cycle patterns ===\n", len(patterns))

	for i, pattern := range patterns {
		result := ExtractCycle(events, pattern.Info)
		centerPct := pattern.CenterPos / float64(len(events)) * 100

		if showSummary {
			fmt.Fprintf(os.Stderr, "\n--- Cycle %d ---\n", i+1)
			fmt.Fprintf(os.Stderr, "Length: %d kernels\n", result.CycleLength)
			fmt.Fprintf(os.Stderr, "Repetitions: %d\n", result.NumCycles)
			fmt.Fprintf(os.Stderr, "Center: %.1f%% of trace\n", centerPct)
			fmt.Fprintf(os.Stderr, "Avg Cycle Time: %.2f µs\n", result.AvgCycleTime)
		}

		if outputBase != "" {
			filename := fmt.Sprintf("%s_cycle_%d.csv", outputBase, i+1)
			if err := result.WriteToFile(filename); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing %s: %v\n", filename, err)
			} else {
				fmt.Fprintf(os.Stderr, "Written: %s\n", filename)
			}
		}
	}

	// If no output specified, write first pattern to stdout
	if outputBase == "" && len(patterns) > 0 {
		result := ExtractCycle(events, patterns[0].Info)
		result.WriteCSV(os.Stdout)
	}
}

func runCompareAll(args []string) {
	compareFlags := flag.NewFlagSet("compare-all", flag.ExitOnError)
	baselineDir := compareFlags.String("baseline", "", "Base path for baseline CSVs (e.g., /tmp/baseline)")
	newDir := compareFlags.String("new", "", "Base path for new CSVs (e.g., /tmp/optimized)")
	outputFile := compareFlags.String("output", "", "Output XLSX file path")

	compareFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uplifter Compare All - Compare all cycle pairs in one XLSX\n\n")
		fmt.Fprintf(os.Stderr, "Usage: uplifter compare-all -baseline <base_path> -new <new_path> -output <file.xlsx>\n\n")
		fmt.Fprintf(os.Stderr, "This compares matching cycle files:\n")
		fmt.Fprintf(os.Stderr, "  <base_path>_cycle_1.csv vs <new_path>_cycle_1.csv\n")
		fmt.Fprintf(os.Stderr, "  <base_path>_cycle_2.csv vs <new_path>_cycle_2.csv\n")
		fmt.Fprintf(os.Stderr, "  ...\n\n")
		fmt.Fprintf(os.Stderr, "Output is a single XLSX with one tab per cycle comparison.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		compareFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  uplifter compare-all -baseline /tmp/baseline -new /tmp/optimized -output comparison.xlsx\n")
	}

	compareFlags.Parse(args)

	if *baselineDir == "" || *newDir == "" || *outputFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -baseline, -new, and -output are required\n\n")
		compareFlags.Usage()
		os.Exit(1)
	}

	// Find all cycle files
	var comparisons []*CompareResult
	var sheetNames []string

	for i := 1; ; i++ {
		baselineFile := fmt.Sprintf("%s_cycle_%d.csv", *baselineDir, i)
		newFile := fmt.Sprintf("%s_cycle_%d.csv", *newDir, i)

		// Check if files exist
		if _, err := os.Stat(baselineFile); os.IsNotExist(err) {
			break
		}
		if _, err := os.Stat(newFile); os.IsNotExist(err) {
			break
		}

		fmt.Fprintf(os.Stderr, "Comparing cycle %d...\n", i)

		result, err := CompareFromCSV(baselineFile, newFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error comparing cycle %d: %v\n", i, err)
			continue
		}

		comparisons = append(comparisons, result)
		sheetNames = append(sheetNames, fmt.Sprintf("Cycle %d", i))
	}

	if len(comparisons) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no cycle files found\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\nWriting %d comparisons to %s...\n", len(comparisons), *outputFile)

	if err := WriteMultiCompareXLSX(*outputFile, comparisons, sheetNames); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing XLSX: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Done! Created %s with %d tabs\n", *outputFile, len(comparisons))
}

// Helper to remove extension from path
func removeExt(path string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext)
}

func runKmerDetection(args []string) {
	kmerFlags := flag.NewFlagSet("kmer", flag.ExitOnError)
	inputFile := kmerFlags.String("input", "", "Input Perfetto trace file (.json or .json.gz)")
	outputBase := kmerFlags.String("output", "", "Output base path for CSV files")

	kmerFlags.Parse(args)

	if *inputFile == "" {
		fmt.Fprintf(os.Stderr, "Error: -input is required\n")
		kmerFlags.Usage()
		os.Exit(1)
	}

	if *outputBase == "" {
		*outputBase = removeExt(*inputFile)
	}

	startTime := time.Now()

	// Parse trace
	fmt.Fprintf(os.Stderr, "Parsing trace file: %s\n", *inputFile)
	events, err := ParseKernelEvents(*inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing trace: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Parsed %d kernel events in %v\n\n", len(events), time.Since(startTime))

	// Detect cycles using k-mer method
	fmt.Fprintf(os.Stderr, "=== Detecting cycles using k-mer method ===\n")
	cycles := DetectCyclesKmer(events, 3, 10)

	if len(cycles) == 0 {
		fmt.Fprintf(os.Stderr, "No cycles detected\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "\n=== Outputting %d cycle patterns ===\n", len(cycles))

	// Output each cycle as CSV
	for i, c := range cycles {
		// Extract cycle statistics
		cycleResult := ExtractCycleStats(events, c.StartIndex, c.Length, c.Repetitions)
		if cycleResult == nil {
			continue
		}

		// Calculate center position
		centerPos := float64(c.StartIndex+c.Length*c.Repetitions/2) / float64(len(events)) * 100

		fmt.Fprintf(os.Stderr, "\n--- Cycle %d ---\n", i+1)
		fmt.Fprintf(os.Stderr, "Length: %d kernels\n", c.Length)
		fmt.Fprintf(os.Stderr, "Repetitions: %d\n", c.Repetitions)
		fmt.Fprintf(os.Stderr, "Center: %.1f%% of trace\n", centerPos)
		fmt.Fprintf(os.Stderr, "Avg Cycle Time: %.2f µs\n", cycleResult.AvgCycleTime)

		// Write CSV
		outPath := fmt.Sprintf("%s_cycle_%d.csv", *outputBase, i+1)
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating file: %v\n", err)
			continue
		}
		if err := cycleResult.WriteCSV(f); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
		}
		f.Close()
		fmt.Fprintf(os.Stderr, "Written: %s\n", outPath)
	}

	fmt.Fprintf(os.Stderr, "\nTotal execution time: %v\n", time.Since(startTime))
}

// ExtractCycleStats extracts statistics for a cycle
func ExtractCycleStats(events []KernelEvent, start, length, reps int) *CycleResult {
	if start+length*reps > len(events) {
		return nil
	}

	// Aggregate statistics for each kernel position in the cycle
	stats := make(map[int]*KernelStats)

	for rep := 0; rep < reps; rep++ {
		for pos := 0; pos < length; pos++ {
			idx := start + rep*length + pos
			if idx >= len(events) {
				break
			}
			e := events[idx]

			if s, exists := stats[pos]; exists {
				s.TotalDur += e.Duration
				s.Count++
				if e.Duration < s.MinDur {
					s.MinDur = e.Duration
				}
				if e.Duration > s.MaxDur {
					s.MaxDur = e.Duration
				}
				s.Durations = append(s.Durations, e.Duration)
			} else {
				stats[pos] = &KernelStats{
					Name:         e.Name,
					TotalDur:     e.Duration,
					MinDur:       e.Duration,
					MaxDur:       e.Duration,
					Count:        1,
					IndexInCycle: pos,
					Durations:    []float64{e.Duration},
				}
			}
		}
	}

	// Calculate averages and build result
	var kernelStats []KernelStats
	var totalCycleTime float64

	for pos := 0; pos < length; pos++ {
		if s, exists := stats[pos]; exists {
			s.AvgDur = s.TotalDur / float64(s.Count)
			s.StdDev = calcStdDev(s.Durations, s.AvgDur)
			totalCycleTime += s.AvgDur
			kernelStats = append(kernelStats, *s)
		}
	}

	return &CycleResult{
		CycleLength:    length,
		NumCycles:      reps,
		Kernels:        kernelStats,
		AvgCycleTime:   totalCycleTime,
		TotalCycleTime: totalCycleTime * float64(reps),
	}
}

// calcStdDev calculates standard deviation
func calcStdDev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var sumSquares float64
	for _, v := range values {
		diff := v - mean
		sumSquares += diff * diff
	}
	variance := sumSquares / float64(len(values)-1)
	return math.Sqrt(variance)
}
