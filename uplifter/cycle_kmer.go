package main

import (
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"strings"
)

// KmerCycle represents a cycle found using k-mer detection
type KmerCycle struct {
	StartIndex  int
	Length      int
	Repetitions int
	AnchorKmer  string // The k-mer used as anchor
}

// DetectCyclesKmer finds cycles using k-mer (kernel sequence) anchors
// Instead of using single kernels as anchors, we use sequences of k consecutive kernels
// This handles cases where the same kernel appears multiple times per cycle
func DetectCyclesKmer(events []KernelEvent, k int, minCycleLen int) []KmerCycle {
	var cycles []KmerCycle
	n := len(events)

	if n < minCycleLen*2+k {
		return cycles
	}

	fmt.Fprintf(os.Stderr, "K-mer cycle detection (k=%d) on %d events...\n", k, n)

	// Step 1: Create k-mers and track their positions
	type kmerInfo struct {
		hash      uint64
		positions []int
		signature string // First 50 chars for display
	}
	kmers := make(map[uint64]*kmerInfo)

	for i := 0; i <= n-k; i++ {
		hash := hashKmer(events, i, k)
		if info, exists := kmers[hash]; exists {
			info.positions = append(info.positions, i)
		} else {
			sig := events[i].Name
			if len(sig) > 50 {
				sig = sig[:50]
			}
			kmers[hash] = &kmerInfo{
				hash:      hash,
				positions: []int{i},
				signature: sig,
			}
		}
	}

	fmt.Fprintf(os.Stderr, "  Created %d unique %d-mers\n", len(kmers), k)

	// Step 2: Find k-mers with regular intervals (good anchors)
	type anchorCandidate struct {
		hash      uint64
		cycleLen  int
		positions []int
		signature string
		score     int // Number of consistent intervals
	}
	var candidates []anchorCandidate

	for _, info := range kmers {
		if len(info.positions) < 5 {
			continue // Need at least 5 occurrences
		}

		// Check if positions have consistent intervals
		cycleLen := info.positions[1] - info.positions[0]
		if cycleLen < minCycleLen {
			continue
		}

		consistent := 0
		for i := 2; i < len(info.positions); i++ {
			diff := info.positions[i] - info.positions[i-1]
			// Allow 10% tolerance
			if abs(diff-cycleLen) <= max(1, cycleLen/10) {
				consistent++
			}
		}

		// Require at least 80% consistent intervals
		if float64(consistent)/float64(len(info.positions)-1) >= 0.8 {
			candidates = append(candidates, anchorCandidate{
				hash:      info.hash,
				cycleLen:  cycleLen,
				positions: info.positions,
				signature: info.signature,
				score:     consistent,
			})
		}
	}

	fmt.Fprintf(os.Stderr, "  Found %d anchor candidates with regular intervals\n", len(candidates))

	if len(candidates) == 0 {
		return cycles
	}

	// Step 3: Sort by score (most consistent first) and group by cycle length
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	// Group candidates by similar cycle lengths
	usedRanges := make(map[int]bool) // Track which event ranges we've used

	for _, cand := range candidates {
		// Check if this range overlaps with already found cycles
		startPos := cand.positions[0]
		if usedRanges[startPos/1000] { // Rough check by 1000-event buckets
			continue
		}

		// Verify this is a real cycle
		reps := verifyKmerCycle(events, cand.positions[0], cand.cycleLen)
		if reps >= 5 {
			cycles = append(cycles, KmerCycle{
				StartIndex:  cand.positions[0],
				Length:      cand.cycleLen,
				Repetitions: reps,
				AnchorKmer:  cand.signature,
			})

			// Mark this range as used
			for i := cand.positions[0]; i < cand.positions[0]+cand.cycleLen*reps; i += 1000 {
				usedRanges[i/1000] = true
			}

			fmt.Fprintf(os.Stderr, "  Found cycle: length=%d, reps=%d, anchor=%s...\n",
				cand.cycleLen, reps, truncateString(cand.signature, 40))
		}
	}

	// Sort cycles by start position
	sort.Slice(cycles, func(i, j int) bool {
		return cycles[i].StartIndex < cycles[j].StartIndex
	})

	// Deduplicate: group cycles by length and merge similar patterns
	cycles = deduplicateCycles(events, cycles)

	fmt.Fprintf(os.Stderr, "Found %d distinct cycles after deduplication\n", len(cycles))
	return cycles
}

// deduplicateCycles removes duplicate cycle patterns
func deduplicateCycles(events []KernelEvent, cycles []KmerCycle) []KmerCycle {
	if len(cycles) == 0 {
		return cycles
	}

	// Group cycles by similar length (within 20%)
	type cycleGroup struct {
		cycles    []KmerCycle
		signature string // Kernel sequence signature
	}
	var groups []cycleGroup

	for _, c := range cycles {
		// Create signature from first 10 kernels
		sig := getCycleSignatureSimple(events, c.StartIndex, c.Length)

		// Find matching group
		found := false
		for i := range groups {
			// Check if lengths are similar (within 20%)
			existingLen := groups[i].cycles[0].Length
			if abs(c.Length-existingLen) <= max(existingLen/5, 2) {
				// Check if signatures match (could be rotated)
				if signaturesMatch(groups[i].signature, sig) {
					groups[i].cycles = append(groups[i].cycles, c)
					found = true
					break
				}
			}
		}

		if !found {
			groups = append(groups, cycleGroup{
				cycles:    []KmerCycle{c},
				signature: sig,
			})
		}
	}

	// For each group, keep the cycle with most repetitions
	var result []KmerCycle
	for _, g := range groups {
		best := g.cycles[0]
		for _, c := range g.cycles[1:] {
			if c.Repetitions > best.Repetitions {
				best = c
			}
		}
		result = append(result, best)
	}

	// Sort by center position (temporal order)
	sort.Slice(result, func(i, j int) bool {
		centerI := result[i].StartIndex + result[i].Length*result[i].Repetitions/2
		centerJ := result[j].StartIndex + result[j].Length*result[j].Repetitions/2
		return centerI < centerJ
	})

	return result
}

// getCycleSignatureSimple creates a simple signature from kernel names
func getCycleSignatureSimple(events []KernelEvent, start, length int) string {
	var parts []string
	count := min(10, length) // Use first 10 kernels
	for i := 0; i < count; i++ {
		name := events[start+i].Name
		// Simplify: take first 30 chars
		if len(name) > 30 {
			name = name[:30]
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, "|")
}

// signaturesMatch checks if two signatures represent the same cycle (possibly rotated)
func signaturesMatch(sig1, sig2 string) bool {
	// Strict check: at least 80% of kernels must match
	parts1 := strings.Split(sig1, "|")
	parts2 := strings.Split(sig2, "|")

	matches := 0
	for _, p1 := range parts1 {
		for _, p2 := range parts2 {
			if p1 == p2 {
				matches++
				break
			}
		}
	}

	// Require 80% match for deduplication
	threshold := len(parts1) * 8 / 10
	if threshold < 1 {
		threshold = 1
	}
	return matches >= threshold
}

// hashKmer creates a hash for k consecutive kernel names
func hashKmer(events []KernelEvent, start, k int) uint64 {
	h := fnv.New64a()
	for i := 0; i < k; i++ {
		h.Write([]byte(events[start+i].Name))
		h.Write([]byte{0}) // Separator
	}
	return h.Sum64()
}

// verifyKmerCycle counts how many times the cycle repeats with 90% match
func verifyKmerCycle(events []KernelEvent, start, length int) int {
	n := len(events)
	reps := 1

	for pos := start + length; pos+length <= n; pos += length {
		matches := 0
		for j := 0; j < length; j++ {
			if events[start+j].Name == events[pos+j].Name {
				matches++
			}
		}
		if float64(matches)/float64(length) >= 0.90 {
			reps++
		} else {
			break
		}
	}
	return reps
}

// TestKmerCycleDetection runs the k-mer algorithm on events and prints results
func TestKmerCycleDetection(events []KernelEvent) {
	fmt.Fprintf(os.Stderr, "\n=== Testing K-mer Cycle Detection ===\n")

	// Try k=3 (3 consecutive kernels as anchor)
	cycles := DetectCyclesKmer(events, 3, 10)

	fmt.Fprintf(os.Stderr, "\nResults:\n")
	for i, c := range cycles {
		fmt.Fprintf(os.Stderr, "  Cycle %d: start=%d, length=%d, reps=%d, anchor=%s...\n",
			i+1, c.StartIndex, c.Length, c.Repetitions, truncateString(c.AnchorKmer, 30))
	}
}

