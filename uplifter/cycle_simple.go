package main

import (
	"fmt"
	"os"
)

// SimpleCycle represents a detected cycle
type SimpleCycle struct {
	StartIndex  int
	Length      int
	Repetitions int
}

// DetectCyclesSimple finds cycles using a simple "detect on repeat" approach
// Algorithm:
// 1. Walk through trace, remember where each kernel was last seen
// 2. When we see a kernel again -> potential cycle
// 3. Verify the sequence repeats
// 4. If yes -> record cycle, reset, continue
func DetectCyclesSimple(events []KernelEvent, minCycleLen int) []SimpleCycle {
	var cycles []SimpleCycle
	n := len(events)
	
	if n < minCycleLen*2 {
		return cycles
	}
	
	fmt.Fprintf(os.Stderr, "Simple cycle detection on %d events (min length: %d)...\n", n, minCycleLen)
	
	pos := 0
	for pos < n-minCycleLen*2 {
		cycle := findNextCycle(events, pos, minCycleLen)
		if cycle != nil {
			cycles = append(cycles, *cycle)
			fmt.Fprintf(os.Stderr, "  Found cycle: start=%d, length=%d, reps=%d\n", 
				cycle.StartIndex, cycle.Length, cycle.Repetitions)
			// Skip past this cycle
			pos = cycle.StartIndex + cycle.Length*cycle.Repetitions
		} else {
			pos++
		}
	}
	
	fmt.Fprintf(os.Stderr, "Found %d cycles\n", len(cycles))
	return cycles
}

// findNextCycle looks for the next cycle starting at or after 'start'
func findNextCycle(events []KernelEvent, start, minLen int) *SimpleCycle {
	n := len(events)
	seen := make(map[string]int) // kernel name -> position
	
	for i := start; i < n; i++ {
		name := events[i].Name
		
		if lastPos, exists := seen[name]; exists {
			cycleLen := i - lastPos
			
			// Skip if too short
			if cycleLen < minLen {
				seen[name] = i
				continue
			}
			
			// Verify: count how many times this sequence repeats
			reps := countRepetitions(events, lastPos, cycleLen)
			
			if reps >= 5 { // Require at least 5 repetitions
				return &SimpleCycle{
					StartIndex:  lastPos,
					Length:      cycleLen,
					Repetitions: reps,
				}
			}
		}
		seen[name] = i
	}
	
	return nil
}

// countRepetitions counts how many times the sequence repeats
func countRepetitions(events []KernelEvent, start, length int) int {
	n := len(events)
	reps := 1 // The first occurrence counts as 1
	
	for pos := start + length; pos+length <= n; pos += length {
		// Check if this segment matches the first
		matches := 0
		for j := 0; j < length; j++ {
			if events[start+j].Name == events[pos+j].Name {
				matches++
			}
		}
		
		// Require 90% match
		if float64(matches)/float64(length) >= 0.90 {
			reps++
		} else {
			break
		}
	}
	
	return reps
}

// TestSimpleCycleDetection runs the simple algorithm on events and prints results
func TestSimpleCycleDetection(events []KernelEvent) {
	fmt.Fprintf(os.Stderr, "\n=== Testing Simple Cycle Detection ===\n")
	
	cycles := DetectCyclesSimple(events, 10)
	
	fmt.Fprintf(os.Stderr, "\nResults:\n")
	for i, c := range cycles {
		fmt.Fprintf(os.Stderr, "  Cycle %d: start=%d, length=%d, reps=%d\n", 
			i+1, c.StartIndex, c.Length, c.Repetitions)
		
		// Print first few kernel names
		fmt.Fprintf(os.Stderr, "    First 5 kernels: ")
		for j := 0; j < 5 && j < c.Length; j++ {
			name := events[c.StartIndex+j].Name
			if len(name) > 30 {
				name = name[:30] + "..."
			}
			fmt.Fprintf(os.Stderr, "\n      %d: %s", j, name)
		}
		fmt.Fprintf(os.Stderr, "\n")
	}
}

