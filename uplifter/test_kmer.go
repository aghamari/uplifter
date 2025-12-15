package main

import (
	"fmt"
	"os"
	"time"
)

// RunKmerTest loads a trace and tests k-mer cycle detection
func RunKmerTest(tracePath string) {
	fmt.Printf("Loading trace: %s\n", tracePath)
	start := time.Now()

	events, err := ParseKernelEvents(tracePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing trace: %v\n", err)
		return
	}
	fmt.Printf("Loaded %d events in %v\n\n", len(events), time.Since(start))

	// Run k-mer detection with k=3
	fmt.Println("=== K-mer Detection (k=3) ===")
	start = time.Now()
	cycles := DetectCyclesKmer(events, 3, 10)
	fmt.Printf("\nK-mer detection took %v\n", time.Since(start))

	fmt.Printf("\n=== Found %d cycles ===\n", len(cycles))
	for i, c := range cycles {
		// Calculate center position
		centerPos := float64(c.StartIndex+c.Length*c.Repetitions/2) / float64(len(events)) * 100

		fmt.Printf("\nCycle %d:\n", i+1)
		fmt.Printf("  Start: %d, Length: %d kernels, Reps: %d\n", c.StartIndex, c.Length, c.Repetitions)
		fmt.Printf("  Center: %.1f%% of trace\n", centerPos)
		fmt.Printf("  Anchor k-mer: %s...\n", truncateString(c.AnchorKmer, 50))

		// Show first 5 kernels
		fmt.Printf("  Kernels:\n")
		for j := 0; j < 5 && j < c.Length; j++ {
			name := events[c.StartIndex+j].Name
			fmt.Printf("    %d: %s\n", j, truncateString(name, 60))
		}
		if c.Length > 5 {
			fmt.Printf("    ... and %d more\n", c.Length-5)
		}
	}
}

