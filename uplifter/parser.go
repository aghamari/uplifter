package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// KernelEvent represents a GPU kernel execution event from the trace
type KernelEvent struct {
	Name      string  `json:"name"`
	Category  string  `json:"cat"`
	Phase     string  `json:"ph"`
	Timestamp float64 `json:"ts"`
	Duration  float64 `json:"dur"`
	Pid       int     `json:"pid"`
	Tid       int     `json:"tid"`
}

// TraceEvent is the raw event from the JSON trace
type TraceEvent struct {
	Name      string                 `json:"name"`
	Category  string                 `json:"cat"`
	Phase     string                 `json:"ph"`
	Timestamp float64                `json:"ts"`
	Duration  float64                `json:"dur"`
	Pid       int                    `json:"pid"`
	Tid       int                    `json:"tid"`
	Args      map[string]interface{} `json:"args,omitempty"`
}

// ParseKernelEvents streams through a Perfetto JSON trace file and extracts kernel events
// It uses streaming JSON parsing to handle large files efficiently
// Supports both .json and .json.gz files
func ParseKernelEvents(filename string) ([]KernelEvent, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var reader io.Reader

	// Check if gzipped
	if strings.HasSuffix(filename, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = bufio.NewReaderSize(gzReader, 64*1024*1024)
	} else {
		reader = bufio.NewReaderSize(file, 64*1024*1024) // 64MB buffer
	}

	decoder := json.NewDecoder(reader)

	// Find the start of the JSON object
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to read initial token: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected JSON object, got %v", token)
	}

	var kernelEvents []KernelEvent

	// Iterate through top-level keys
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("failed to read key token: %w", err)
		}

		key, ok := keyToken.(string)
		if !ok {
			continue
		}

		if key == "traceEvents" {
			// Found the traceEvents array - stream through it
			events, err := parseTraceEventsArray(decoder)
			if err != nil {
				return nil, fmt.Errorf("failed to parse traceEvents: %w", err)
			}
			kernelEvents = events
		} else {
			// Skip other fields by reading and discarding their values
			var skip json.RawMessage
			if err := decoder.Decode(&skip); err != nil {
				return nil, fmt.Errorf("failed to skip field %s: %w", key, err)
			}
		}
	}

	return kernelEvents, nil
}

// parseTraceEventsArray streams through the traceEvents array and extracts kernel events
func parseTraceEventsArray(decoder *json.Decoder) ([]KernelEvent, error) {
	// Expect array start
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to read array start: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return nil, fmt.Errorf("expected array start, got %v", token)
	}

	var kernelEvents []KernelEvent
	eventCount := 0
	kernelCount := 0

	// Stream through array elements
	for decoder.More() {
		var event TraceEvent
		if err := decoder.Decode(&event); err != nil {
			// Skip malformed events
			continue
		}
		eventCount++

		// Filter for kernel events only
		if event.Category == "kernel" && event.Phase == "X" {
			kernelEvents = append(kernelEvents, KernelEvent{
				Name:      event.Name,
				Category:  event.Category,
				Phase:     event.Phase,
				Timestamp: event.Timestamp,
				Duration:  event.Duration,
				Pid:       event.Pid,
				Tid:       event.Tid,
			})
			kernelCount++
		}

		// Progress indicator for large files
		if eventCount%500000 == 0 {
			fmt.Fprintf(os.Stderr, "\rProcessed %d events, found %d kernels...", eventCount, kernelCount)
		}
	}

	if eventCount > 500000 {
		fmt.Fprintf(os.Stderr, "\rProcessed %d events, found %d kernels. Done.\n", eventCount, kernelCount)
	}

	// Read array end
	_, err = decoder.Token()
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read array end: %w", err)
	}

	return kernelEvents, nil
}

// ParseKernelEventsWithCallback streams through the trace and calls callback for each kernel
// This is more memory efficient for very large traces
// Supports both .json and .json.gz files
func ParseKernelEventsWithCallback(filename string, callback func(KernelEvent) bool) error {
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var reader io.Reader

	// Check if gzipped
	if strings.HasSuffix(filename, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = bufio.NewReaderSize(gzReader, 64*1024*1024)
	} else {
		reader = bufio.NewReaderSize(file, 64*1024*1024)
	}

	decoder := json.NewDecoder(reader)

	// Find the start of the JSON object
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("failed to read initial token: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("expected JSON object, got %v", token)
	}

	// Iterate through top-level keys
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("failed to read key token: %w", err)
		}

		key, ok := keyToken.(string)
		if !ok {
			continue
		}

		if key == "traceEvents" {
			return streamTraceEvents(decoder, callback)
		} else {
			var skip json.RawMessage
			if err := decoder.Decode(&skip); err != nil {
				return fmt.Errorf("failed to skip field %s: %w", key, err)
			}
		}
	}

	return nil
}

func streamTraceEvents(decoder *json.Decoder, callback func(KernelEvent) bool) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("failed to read array start: %w", err)
	}
	if delim, ok := token.(json.Delim); !ok || delim != '[' {
		return fmt.Errorf("expected array start, got %v", token)
	}

	for decoder.More() {
		var event TraceEvent
		if err := decoder.Decode(&event); err != nil {
			continue
		}

		if event.Category == "kernel" && event.Phase == "X" {
			shouldContinue := callback(KernelEvent{
				Name:      event.Name,
				Category:  event.Category,
				Phase:     event.Phase,
				Timestamp: event.Timestamp,
				Duration:  event.Duration,
				Pid:       event.Pid,
				Tid:       event.Tid,
			})
			if !shouldContinue {
				return nil
			}
		}
	}

	return nil
}

// ParseWithEarlyStop streams through the trace and stops parsing once a cycle is detected
// This is more efficient for large traces with repeating patterns
func ParseWithEarlyStop(filename string, minCycle, maxCycle int) ([]KernelEvent, error) {
	var events []KernelEvent
	kernelCount := 0
	checkInterval := 10000 // Check for cycles every N kernels
	minEventsForDetection := maxInt(minCycle*5, 1000) // Need at least 5 potential cycles

	err := ParseKernelEventsWithCallback(filename, func(event KernelEvent) bool {
		events = append(events, event)
		kernelCount++

		// Progress indicator
		if kernelCount%50000 == 0 {
			fmt.Fprintf(os.Stderr, "\rCollected %d kernels, checking for cycles...", kernelCount)
		}

		// Periodically check if we've found a cycle
		if kernelCount >= minEventsForDetection && kernelCount%checkInterval == 0 {
			// Try to detect a cycle in what we have so far
			cycleInfo := tryEarlyDetection(events, minCycle, minInt(maxCycle, len(events)/3))
			if cycleInfo != nil && cycleInfo.NumCycles >= 10 {
				// Found a confident cycle with 10+ reps (skip warmup patterns), we can stop
				fmt.Fprintf(os.Stderr, "\rEarly stop: detected cycle of length %d with %d repetitions (at %d kernels)\n",
					cycleInfo.CycleLength, cycleInfo.NumCycles, kernelCount)
				return false // Stop parsing
			}
		}

		return true // Continue parsing
	})

	if err != nil {
		return nil, err
	}

	if kernelCount > 50000 {
		fmt.Fprintf(os.Stderr, "\rCollected %d kernels. Done.\n", kernelCount)
	}

	return events, nil
}

// tryEarlyDetection attempts a quick cycle detection for early stopping
func tryEarlyDetection(events []KernelEvent, minCycle, maxCycle int) *CycleInfo {
	if len(events) < minCycle*3 {
		return nil
	}

	// Use a fast heuristic: look for frequently repeating kernels at regular intervals
	counts := make(map[string][]int)
	for i, e := range events {
		counts[e.Name] = append(counts[e.Name], i)
	}

	// Find the most promising anchor (appears at regular intervals)
	for _, positions := range counts {
		if len(positions) < 5 {
			continue
		}

		// Check if positions are evenly spaced
		gaps := make([]int, len(positions)-1)
		for i := 1; i < len(positions); i++ {
			gaps[i-1] = positions[i] - positions[i-1]
		}

		// Find the most common gap
		gapCounts := make(map[int]int)
		for _, gap := range gaps {
			if gap >= minCycle && gap <= maxCycle {
				gapCounts[gap]++
			}
		}

		for gap, count := range gapCounts {
			if count >= 4 { // At least 4 consistent repetitions
				// Verify this is a real cycle
				info := verifyCycleQuick(events, gap, positions[0])
				if info != nil && info.NumCycles >= 5 {
					return info
				}
			}
		}
	}

	return nil
}

// verifyCycleQuick does a quick cycle verification for early stopping
func verifyCycleQuick(events []KernelEvent, cycleLen, startIdx int) *CycleInfo {
	if startIdx+cycleLen*3 > len(events) {
		return nil
	}

	// Hash the first cycle
	firstCycleHashes := make([]uint64, cycleLen)
	for i := 0; i < cycleLen && startIdx+i < len(events); i++ {
		firstCycleHashes[i] = hashString(events[startIdx+i].Name)
	}

	// Check how many cycles match
	matches := 1
	cycleIndices := []int{startIdx}
	
	for pos := startIdx + cycleLen; pos+cycleLen <= len(events); pos += cycleLen {
		matchCount := 0
		for i := 0; i < cycleLen; i++ {
			h := hashString(events[pos+i].Name)
			if h == firstCycleHashes[i] {
				matchCount++
			}
		}

		// Require 90% match for early detection
		if float64(matchCount)/float64(cycleLen) >= 0.90 {
			matches++
			cycleIndices = append(cycleIndices, pos)
		} else {
			break
		}
	}

	if matches >= 5 {
		return &CycleInfo{
			StartIndex:   startIdx,
			CycleLength:  cycleLen,
			NumCycles:    matches,
			CycleIndices: cycleIndices,
		}
	}

	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

