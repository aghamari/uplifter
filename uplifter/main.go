package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	// Check for subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "compare":
			runCompare(os.Args[2:])
			return
		case "compare-csv":
			runCompareCSV(os.Args[2:])
			return
		}
	}

	// Default: cycle detection mode
	runCycleDetection()
}

func runCompare(args []string) {
	compareFlags := flag.NewFlagSet("compare", flag.ExitOnError)
	trace1 := compareFlags.String("trace1", "", "Path to first trace file (e.g., eager mode)")
	trace2 := compareFlags.String("trace2", "", "Path to second trace file (e.g., compiled mode)")
	outputFile := compareFlags.String("output", "", "Output CSV file path")
	fullParse := compareFlags.Bool("full", false, "Parse entire trace files (default: early stop)")
	showSummary := compareFlags.Bool("summary", true, "Print summary to stderr")
	phase := compareFlags.String("phase", "decode", "Phase to analyze: auto, prefill, decode (default: decode)")

	compareFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uplifter Compare - Compare kernel cycles between two traces\n\n")
		fmt.Fprintf(os.Stderr, "Usage: uplifter compare -trace1 <file1> -trace2 <file2> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		compareFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  uplifter compare -trace1 eager.json.gz -trace2 compiled.json.gz -output compare.csv\n")
		fmt.Fprintf(os.Stderr, "  uplifter compare -trace1 eager.json.gz -trace2 compiled.json.gz -phase prefill -full -output compare.xlsx\n")
	}

	compareFlags.Parse(args)

	if *trace1 == "" || *trace2 == "" {
		fmt.Fprintf(os.Stderr, "Error: -trace1 and -trace2 are required\n\n")
		compareFlags.Usage()
		os.Exit(1)
	}

	// Set global phase mode
	PhaseMode = *phase

	startTime := time.Now()

	result, err := CompareTraces(*trace1, *trace2, *fullParse)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error comparing traces: %v\n", err)
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
		// Write to stdout
		result.WriteCompareCSV(os.Stdout)
	}

	fmt.Fprintf(os.Stderr, "Total execution time: %v\n", time.Since(startTime))
}

func runCompareCSV(args []string) {
	compareFlags := flag.NewFlagSet("compare-csv", flag.ExitOnError)
	csv1 := compareFlags.String("eager", "", "Path to eager mode CSV (from uplifter)")
	csv2 := compareFlags.String("compiled", "", "Path to compiled mode CSV (from uplifter)")
	outputFile := compareFlags.String("output", "", "Output CSV file path")
	showSummary := compareFlags.Bool("summary", true, "Print summary to stderr")

	compareFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uplifter Compare CSV - Fast comparison using pre-extracted CSVs\n\n")
		fmt.Fprintf(os.Stderr, "Usage: uplifter compare-csv -eager <eager.csv> -compiled <compiled.csv> [options]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		compareFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # First extract sub-cycles from each trace:\n")
		fmt.Fprintf(os.Stderr, "  uplifter -input eager.json.gz -output eager.csv\n")
		fmt.Fprintf(os.Stderr, "  uplifter -input compiled.json.gz -output compiled.csv\n")
		fmt.Fprintf(os.Stderr, "\n  # Then compare them:\n")
		fmt.Fprintf(os.Stderr, "  uplifter compare-csv -eager eager.csv -compiled compiled.csv -output comparison.csv\n")
	}

	compareFlags.Parse(args)

	if *csv1 == "" || *csv2 == "" {
		fmt.Fprintf(os.Stderr, "Error: -eager and -compiled are required\n\n")
		compareFlags.Usage()
		os.Exit(1)
	}

	startTime := time.Now()

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
	outputFile := flag.String("output", "", "Output file path (.csv, .json, or .txt for summary)")
	minCycle := flag.Int("min-cycle", 50, "Minimum cycle length to search for")
	maxCycle := flag.Int("max-cycle", 5000, "Maximum cycle length to search for")
	autoDetect := flag.Bool("auto", true, "Use automatic cycle detection (signature-based)")
	showSummary := flag.Bool("summary", true, "Print summary to stderr")
	normalize := flag.Bool("normalize", false, "Normalize kernel names (strip triton suffix numbers) to detect smaller cycles")
	fullParse := flag.Bool("full", false, "Parse entire trace file (default: early stop after detecting cycle)")
	phase := flag.String("phase", "auto", "Phase to detect: 'prefill', 'decode', or 'auto' (auto prefers more repetitions)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uplifter - Perfetto trace cycle detector and comparator\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s -input <trace.json> [options]     Detect kernel cycles\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s compare -trace1 <f1> -trace2 <f2> Compare two traces\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Cycle Detection Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s -input trace.json -output kernels.csv\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s compare -trace1 eager.json.gz -trace2 compiled.json.gz\n", os.Args[0])
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

	// Step 1: Parse kernel events from the trace
	var events []KernelEvent
	var err error
	
	if *fullParse {
		fmt.Fprintf(os.Stderr, "Parsing entire trace file: %s\n", *inputFile)
		events, err = ParseKernelEvents(*inputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing trace: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Parsing with early stopping: %s\n", *inputFile)
		events, err = ParseWithEarlyStop(*inputFile, *minCycle, *maxCycle)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing trace: %v\n", err)
			os.Exit(1)
		}
	}

	parseTime := time.Since(startTime)
	fmt.Fprintf(os.Stderr, "Parsed %d kernel events in %v\n", len(events), parseTime)

	if len(events) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no kernel events found in trace\n")
		os.Exit(1)
	}

	// Set normalization mode
	NormalizeNames = *normalize
	if *normalize {
		fmt.Fprintf(os.Stderr, "Kernel name normalization enabled (triton suffixes stripped)\n")
	}

	// Set phase detection mode
	PhaseMode = *phase
	if *phase != "auto" {
		fmt.Fprintf(os.Stderr, "Phase detection mode: %s\n", *phase)
	}

	// Step 2: Detect cycles
	var cycleInfo *CycleInfo
	if *autoDetect {
		cycleInfo, err = DetectCycleBySignature(events)
	} else {
		cycleInfo, err = DetectCycle(events, *minCycle, *maxCycle)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error detecting cycle: %v\n", err)
		fmt.Fprintf(os.Stderr, "Try adjusting -min-cycle and -max-cycle parameters, or use -auto=false\n")
		os.Exit(1)
	}

	detectTime := time.Since(startTime) - parseTime
	fmt.Fprintf(os.Stderr, "Cycle detected in %v\n", detectTime)

	// Step 3: Extract cycle statistics
	result := ExtractCycle(events, cycleInfo)

	// Step 4: Output results
	if *showSummary {
		result.WriteSummary(os.Stderr)
	}

	if *outputFile != "" {
		err = result.WriteToFile(*outputFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Results written to: %s\n", *outputFile)
	} else {
		// Default: write CSV to stdout
		result.WriteCSV(os.Stdout)
	}

	totalTime := time.Since(startTime)
	fmt.Fprintf(os.Stderr, "\nTotal execution time: %v\n", totalTime)
}

