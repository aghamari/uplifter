package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"
)

// CycleInfo contains information about a detected cycle
type CycleInfo struct {
	StartIndex   int   // Index where the first complete cycle starts
	CycleLength  int   // Number of kernels in one cycle
	NumCycles    int   // Number of complete cycles found
	CycleIndices []int // Start indices of each detected cycle
}

// KernelStats contains aggregated statistics for a kernel in the cycle
type KernelStats struct {
	Name         string
	TotalDur     float64
	MinDur       float64
	MaxDur       float64
	Count        int
	AvgDur       float64
	IndexInCycle int // Position within the cycle
}

// NormalizeNames controls whether kernel names are normalized before comparison
var NormalizeNames = false

// PhaseMode controls which phase to detect: "auto", "prefill", or "decode"
// Detection is based on REPETITION COUNT (model-agnostic):
// - decode = cycle with MOST repetitions (generates many tokens)
// - prefill = cycle with FEWER repetitions (processes prompt once)
var PhaseMode = "auto"

// DetectCycle finds repeating cycles in a sequence of kernel events
// It uses a rolling hash approach to efficiently find repeating patterns
func DetectCycle(events []KernelEvent, minCycleLen, maxCycleLen int) (*CycleInfo, error) {
	if len(events) < minCycleLen*2 {
		return nil, fmt.Errorf("not enough events (%d) for cycle detection (need at least %d)", len(events), minCycleLen*2)
	}

	// Create a sequence of hashed kernel names for faster comparison
	hashes := make([]uint64, len(events))
	for i, e := range events {
		if NormalizeNames {
			hashes[i] = hashStringNormalized(e.Name)
		} else {
			hashes[i] = hashString(e.Name)
		}
	}

	fmt.Fprintf(os.Stderr, "Searching for cycles (length %d-%d) in %d kernel events...\n", minCycleLen, maxCycleLen, len(events))

	// Try different cycle lengths, starting from minimum
	for cycleLen := minCycleLen; cycleLen <= maxCycleLen && cycleLen <= len(events)/2; cycleLen++ {
		info := tryCycleLength(hashes, events, cycleLen)
		if info != nil && info.NumCycles >= 2 {
			fmt.Fprintf(os.Stderr, "Found cycle of length %d repeating %d times\n", cycleLen, info.NumCycles)
			return info, nil
		}

		// Progress indicator
		if cycleLen%100 == 0 {
			fmt.Fprintf(os.Stderr, "\rTrying cycle length %d...", cycleLen)
		}
	}

	return nil, fmt.Errorf("no repeating cycle found in range [%d, %d]", minCycleLen, maxCycleLen)
}

// tryCycleLength checks if the sequence repeats with the given cycle length
func tryCycleLength(hashes []uint64, events []KernelEvent, cycleLen int) *CycleInfo {
	n := len(hashes)

	// Try different starting positions (to handle warm-up phase)
	for startOffset := 0; startOffset < cycleLen && startOffset < n/4; startOffset++ {
		matches := 0
		cycleIndices := []int{startOffset}

		// Count how many complete cycles match
		for pos := startOffset; pos+cycleLen <= n; pos += cycleLen {
			if pos > startOffset {
				// Check if this segment matches the first cycle
				isMatch := true
				for i := 0; i < cycleLen; i++ {
					if hashes[startOffset+i] != hashes[pos+i] {
						isMatch = false
						break
					}
				}
				if isMatch {
					matches++
					cycleIndices = append(cycleIndices, pos)
				} else {
					// Allow one mismatch and continue checking
					break
				}
			} else {
				matches++
			}
		}

		// Found a good cycle
		if matches >= 2 {
			return &CycleInfo{
				StartIndex:   startOffset,
				CycleLength:  cycleLen,
				NumCycles:    matches,
				CycleIndices: cycleIndices,
			}
		}
	}

	return nil
}

// DetectCycleAuto automatically determines cycle length using autocorrelation-like approach
func DetectCycleAuto(events []KernelEvent) (*CycleInfo, error) {
	if len(events) < 20 {
		return nil, fmt.Errorf("not enough events for auto cycle detection")
	}

	fmt.Fprintf(os.Stderr, "Auto-detecting cycle in %d kernel events...\n", len(events))

	// Find potential cycle length by looking for repeated subsequences
	// Start by finding the first occurrence of a repeated kernel name
	firstRepeat := findFirstRepeat(events)
	if firstRepeat == 0 {
		return nil, fmt.Errorf("no repeated kernel found")
	}

	// Search around the first repeat position
	minLen := max(10, firstRepeat-100)
	maxLen := min(len(events)/2, firstRepeat+1000)

	return DetectCycle(events, minLen, maxLen)
}

// DetectCycleBySignature uses a signature-based approach
// It looks for a unique "anchor" kernel that appears periodically
// and finds the MINIMUM cycle length (smallest repeating unit)
func DetectCycleBySignature(events []KernelEvent) (*CycleInfo, error) {
	if len(events) < 20 {
		return nil, fmt.Errorf("not enough events")
	}

	// First, find the outer cycle using exact matching
	outerCycle := findOuterCycle(events)

	// Then look for sub-cycles within the outer cycle using type-based matching
	if outerCycle != nil && outerCycle.CycleLength > 20 {
		fmt.Fprintf(os.Stderr, "Found outer cycle: length=%d, repetitions=%d\n",
			outerCycle.CycleLength, outerCycle.NumCycles)
		fmt.Fprintf(os.Stderr, "Looking for sub-cycles within outer cycle...\n")

		// Extract one cycle's worth of events
		cycleEvents := events[outerCycle.StartIndex : outerCycle.StartIndex+outerCycle.CycleLength]
		subCycle := findSubCycle(cycleEvents, events, outerCycle)
		if subCycle != nil {
			fmt.Fprintf(os.Stderr, "Found sub-cycle: length=%d, repetitions=%d\n",
				subCycle.CycleLength, subCycle.NumCycles)
			return subCycle, nil
		}
	}

	if outerCycle != nil {
		return outerCycle, nil
	}

	return DetectCycleAuto(events)
}

// findOuterCycle finds repeating cycles using exact kernel name matching
// Uses REPETITION COUNT for phase detection (model-agnostic):
// - "decode": Find cycle with MOST repetitions (token generation = many iterations)
// - "prefill": Find cycle with FEWER repetitions (prompt processing = few iterations)
// - "auto": Same as decode (prefer main workload)
func findOuterCycle(events []KernelEvent) *CycleInfo {
	// Count kernel occurrences
	counts := make(map[string]int)
	for _, e := range events {
		counts[e.Name]++
	}

	// Find kernels that appear multiple times but not too frequently
	type candidate struct {
		name     string
		count    int
		cycleLen int
	}
	var candidates []candidate
	for name, count := range counts {
		if count >= 5 && count <= len(events)/5 { // Require at least 5 occurrences
			estimatedCycleLen := len(events) / count
			candidates = append(candidates, candidate{name, count, estimatedCycleLen})
		}
	}

	// Sort by count based on PhaseMode
	switch PhaseMode {
	case "prefill":
		// FEWER repetitions = prefill (prompt processing runs once/few times)
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].count < candidates[j].count
		})
	default: // "decode" or "auto" - MORE repetitions = decode (many tokens generated)
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].count > candidates[j].count
		})
	}

	// Find valid cycles, collect all of them
	type validCycle struct {
		info   *CycleInfo
		anchor string
	}
	var validCycles []validCycle

	for _, cand := range candidates {
		positions := findKernelPositions(events, cand.name)
		if len(positions) < 5 {
			continue
		}

		cycleLen := positions[1] - positions[0]
		if cycleLen < 10 {
			continue
		}

		isConsistent := true
		consistentCount := 1
		for i := 2; i < len(positions); i++ {
			diff := positions[i] - positions[i-1]
			if abs(diff-cycleLen) > max(1, cycleLen/20) {
				isConsistent = false
				break
			}
			consistentCount++
		}

		if isConsistent && consistentCount >= 5 {
			info := verifyCycle(events, positions[0], cycleLen, len(positions))
			if info != nil && info.NumCycles >= 5 {
				validCycles = append(validCycles, validCycle{info, cand.name})
			}
		}
	}

	if len(validCycles) == 0 {
		return nil
	}

	// Sort valid cycles by repetition count
	switch PhaseMode {
	case "prefill":
		// Return cycle with FEWEST repetitions
		sort.Slice(validCycles, func(i, j int) bool {
			return validCycles[i].info.NumCycles < validCycles[j].info.NumCycles
		})
		fmt.Fprintf(os.Stderr, "Found PREFILL cycle: %d reps (anchor: %s)\n",
			validCycles[0].info.NumCycles, truncateName(validCycles[0].anchor, 40))
	default: // "decode" or "auto"
		// Return cycle with MOST repetitions
		sort.Slice(validCycles, func(i, j int) bool {
			return validCycles[i].info.NumCycles > validCycles[j].info.NumCycles
		})
		fmt.Fprintf(os.Stderr, "Found DECODE cycle: %d reps (anchor: %s)\n",
			validCycles[0].info.NumCycles, truncateName(validCycles[0].anchor, 40))
	}

	return validCycles[0].info
}

// truncateName shortens a string for display
func truncateName(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// findSubCycle looks for repeating patterns within a cycle using kernel type signatures
func findSubCycle(cycleEvents []KernelEvent, allEvents []KernelEvent, outerCycle *CycleInfo) *CycleInfo {
	n := len(cycleEvents)

	// Create type signatures for each kernel (simplified names for pattern matching)
	signatures := make([]string, n)
	for i, e := range cycleEvents {
		signatures[i] = getKernelSignature(e.Name)
	}

	// Find kernels that repeat within the cycle
	sigCounts := make(map[string][]int) // signature -> positions within cycle
	for i, sig := range signatures {
		sigCounts[sig] = append(sigCounts[sig], i)
	}

	// Look for signatures that appear multiple times at regular intervals
	var bestSubCycleLen int
	var bestPositions []int

	for sig, positions := range sigCounts {
		if len(positions) < 3 {
			continue
		}

		// Check if positions are evenly spaced
		subCycleLen := positions[1] - positions[0]
		if subCycleLen < 5 || subCycleLen >= n/2 {
			continue
		}

		isConsistent := true
		for i := 2; i < len(positions); i++ {
			diff := positions[i] - positions[i-1]
			if abs(diff-subCycleLen) > max(1, subCycleLen/10) {
				isConsistent = false
				break
			}
		}

		if isConsistent && (bestSubCycleLen == 0 || subCycleLen < bestSubCycleLen) {
			// Verify the sub-cycle using signatures
			if verifySubCycleBySignature(signatures, positions[0], subCycleLen) {
				bestSubCycleLen = subCycleLen
				bestPositions = positions
				fmt.Fprintf(os.Stderr, "  Sub-cycle candidate: length=%d (anchor: %s)\n",
					subCycleLen, truncateString(sig, 40))
			}
		}
	}

	if bestSubCycleLen > 0 {
		// Calculate total repetitions across all outer cycles
		totalReps := len(bestPositions) * outerCycle.NumCycles

		// Build cycle indices across all events
		var cycleIndices []int
		for _, outerStart := range outerCycle.CycleIndices {
			for _, posInCycle := range bestPositions {
				cycleIndices = append(cycleIndices, outerStart+posInCycle)
			}
		}

		return &CycleInfo{
			StartIndex:   outerCycle.StartIndex + bestPositions[0],
			CycleLength:  bestSubCycleLen,
			NumCycles:    totalReps,
			CycleIndices: cycleIndices,
		}
	}

	return nil
}

// verifySubCycleBySignature checks if the signature pattern repeats
func verifySubCycleBySignature(signatures []string, startIdx, cycleLen int) bool {
	n := len(signatures)
	matches := 0
	checks := 0

	for i := startIdx; i+cycleLen < n; i += cycleLen {
		checks++
		matchCount := 0
		for j := 0; j < cycleLen && i+j < n && i+j+cycleLen < n; j++ {
			if signatures[i+j] == signatures[i+j+cycleLen] {
				matchCount++
			}
		}
		// Require 80% signature match for sub-cycles (more lenient than exact)
		if float64(matchCount)/float64(cycleLen) >= 0.80 {
			matches++
		}
	}

	// Need at least 3 matching repetitions
	return matches >= 3
}

// getKernelSignature returns a simplified signature for a kernel name
// This groups similar kernels together for pattern detection
func getKernelSignature(name string) string {
	// Generic signature extraction without hardcoding kernel names
	// Strategy: extract the base kernel name by removing:
	// 1. Template parameters (content in <>)
	// 2. Configuration suffixes (like _GROUP_K_128_...)
	// 3. Trailing numbers

	sig := name

	// Remove template parameters - find first < and truncate
	if idx := strings.Index(sig, "<"); idx > 0 {
		sig = sig[:idx]
	}

	// Remove common configuration suffixes
	// These patterns typically indicate compile-time parameters, not different kernels
	configPatterns := []string{
		"_GROUP_K_", "_GROUP_N_", "_BLOCK_SIZE_", "_GRID_",
		"_MT", "_MI", "_SN_", "_AFC", "_LDSB", "_LPA", "_LPB",
		"_UserArgs_", "_shortname",
	}
	for _, pattern := range configPatterns {
		if idx := strings.Index(sig, pattern); idx > 0 {
			sig = sig[:idx]
		}
	}

	// Remove trailing numbers (like _0, _1, _9)
	for len(sig) > 2 && sig[len(sig)-1] >= '0' && sig[len(sig)-1] <= '9' && sig[len(sig)-2] == '_' {
		sig = sig[:len(sig)-2]
	}

	// Clean up any trailing underscores
	sig = strings.TrimRight(sig, "_")

	// If signature is empty or too short, use a hash
	if len(sig) < 3 {
		return fmt.Sprintf("other_%d", hashString(name)%1000)
	}

	return sig
}

func findKernelPositions(events []KernelEvent, name string) []int {
	var positions []int
	for i, e := range events {
		eName := e.Name
		if NormalizeNames {
			eName = normalizeKernelName(eName)
		}
		if eName == name {
			positions = append(positions, i)
		}
	}
	return positions
}

func verifyCycle(events []KernelEvent, startIdx, cycleLen, expectedCycles int) *CycleInfo {
	hashes := make([]uint64, len(events))
	for i, e := range events {
		if NormalizeNames {
			hashes[i] = hashStringNormalized(e.Name)
		} else {
			hashes[i] = hashString(e.Name)
		}
	}

	cycleIndices := []int{startIdx}
	matches := 1

	for i := 1; i < expectedCycles; i++ {
		pos := startIdx + i*cycleLen
		if pos+cycleLen > len(events) {
			break
		}

		// Check match with tolerance for slight variations
		matchCount := 0
		for j := 0; j < cycleLen; j++ {
			if hashes[startIdx+j] == hashes[pos+j] {
				matchCount++
			}
		}

		// Require 95% match
		if float64(matchCount)/float64(cycleLen) >= 0.95 {
			matches++
			cycleIndices = append(cycleIndices, pos)
		}
	}

	if matches >= 2 {
		return &CycleInfo{
			StartIndex:   startIdx,
			CycleLength:  cycleLen,
			NumCycles:    matches,
			CycleIndices: cycleIndices,
		}
	}
	return nil
}

func findFirstRepeat(events []KernelEvent) int {
	seen := make(map[uint64]int)
	for i, e := range events {
		var h uint64
		if NormalizeNames {
			h = hashStringNormalized(e.Name)
		} else {
			h = hashString(e.Name)
		}
		if _, exists := seen[h]; exists {
			return i
		}
		seen[h] = i
	}
	return 0
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// hashStringNormalized hashes a kernel name after normalizing it
// This strips trailing numbers from triton kernels to group similar kernels
func hashStringNormalized(s string) uint64 {
	normalized := normalizeKernelName(s)
	h := fnv.New64a()
	h.Write([]byte(normalized))
	return h.Sum64()
}

// normalizeKernelName removes variable parts from kernel names
// e.g., "triton_red_fused_something_123" -> "triton_red_fused_something"
func normalizeKernelName(name string) string {
	// For triton kernels, strip trailing _N suffix
	if len(name) > 7 && name[:7] == "triton_" {
		// Find last underscore followed by digits
		lastUnderscore := -1
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '_' {
				// Check if everything after is digits
				allDigits := true
				for j := i + 1; j < len(name); j++ {
					if name[j] < '0' || name[j] > '9' {
						allDigits = false
						break
					}
				}
				if allDigits && i+1 < len(name) {
					lastUnderscore = i
					break
				}
			}
		}
		if lastUnderscore > 0 {
			return name[:lastUnderscore]
		}
	}
	return name
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
