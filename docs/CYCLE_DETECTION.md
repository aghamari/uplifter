# Cycle Detection Algorithm

This document provides a detailed explanation of how the `uplifter` tool detects repeating kernel cycles in Perfetto traces and distinguishes between prefill and decode phases.

## Overview

The cycle detection process has three main stages:
1. **Find All Cycle Patterns**: Detect all distinct repeating patterns across the entire trace
2. **Classify by Position**: Group patterns by signature and classify based on temporal position
3. **Sub-Cycle Detection**: Find smaller repeating patterns within outer cycles (e.g., transformer layers)

## Data Structures

### CycleInfo

```go
type CycleInfo struct {
    StartIndex   int   // Index where the first complete cycle starts
    CycleLength  int   // Number of kernels in one cycle
    NumCycles    int   // Number of complete cycles found
    CycleIndices []int // Start indices of each detected cycle
}
```

### CyclePattern

```go
type CyclePattern struct {
    Info      *CycleInfo
    Signature string    // Kernel signature for grouping similar patterns
    StartPos  int       // First occurrence position in trace
    EndPos    int       // Last occurrence position in trace
    CenterPos float64   // Average position (for phase classification)
    Anchor    string    // Anchor kernel name used for detection
}
```

## Entry Point: `DetectCycleBySignature`

The main entry point is `DetectCycleBySignature(events []KernelEvent)`. The behavior depends on the global `PhaseMode` variable:

| PhaseMode | Behavior |
|-----------|----------|
| `"auto"` | Standard detection (same as decode) |
| `"decode"` | Find all patterns, select the one with **latest** center position |
| `"prefill"` | Find all patterns, select the one with **earliest** center position |

```go
switch PhaseMode {
case "prefill", "decode":
    result, err = detectPhaseByAllCycles(events, PhaseMode)
    if err != nil || result == nil {
        // Fallback to standard detection
        result, err = detectCycleStandard(events, 0)
    }
default: // "auto"
    result, err = detectCycleStandard(events, 0)
}
```

---

## Phase Detection: `detectPhaseByAllCycles`

This function finds ALL distinct cycle patterns in the trace, then classifies them by temporal position.

### Rationale

In LLM inference:
- **Prefill** (prompt processing) happens at the **beginning** of execution
- **Decode** (token generation) happens at the **end** and dominates execution time

Instead of using fixed percentage boundaries (which can fail if phases overlap), we:
1. Find ALL cycle patterns across the entire trace
2. Calculate each pattern's "center position" (average of start and end)
3. Select based on position: earliest = prefill, latest = decode

### Algorithm

```go
func detectPhaseByAllCycles(events []KernelEvent, phase string) (*CycleInfo, error) {
    // 1. Find all distinct cycle patterns
    patterns := findAllCyclePatterns(events)
    
    // 2. Sort by center position (earliest first)
    sort.Slice(patterns, func(i, j int) bool {
        return patterns[i].CenterPos < patterns[j].CenterPos
    })
    
    // 3. Select based on phase
    if phase == "prefill" {
        return patterns[0].Info, nil  // Earliest center
    } else {
        return patterns[len(patterns)-1].Info, nil  // Latest center
    }
}
```

### Example Output

```
Found 7 distinct cycle patterns:
  1. length=17, reps=93, center=98.9%, sig=void wvSplitK_hf_sml|...
  2. length=18, reps=3384, center=9.0%, sig=aiter::fmoe_bf16_...
  3. length=25, reps=1410, center=2.7%, sig=aiter::fmha_fwd_...
  4. length=17, reps=1034, center=91.5%, sig=void tensorrt_llm::...
  ...
Selected DECODE pattern: center=98.9%, length=17, reps=93
```

---

## Finding All Cycle Patterns: `findAllCyclePatterns`

This function finds all distinct repeating patterns in the trace.

### Step 1: Count Kernel Occurrences

```go
counts := make(map[string]int)
for _, e := range events {
    counts[e.Name]++
}
```

### Step 2: Find Anchor Candidates

An "anchor" kernel is one that appears at regular intervals and can be used to identify cycle boundaries.

**Criteria for candidates:**
- Must appear at least 5 times
- Must appear no more than `len(events)/5` times (not too frequent)

```go
var candidates []candidate
for name, count := range counts {
    if count >= 5 && count <= len(events)/5 {
        estimatedCycleLen := len(events) / count
        candidates = append(candidates, candidate{name, count, estimatedCycleLen})
    }
}
```

### Step 3: Sort Candidates by Repetition Count

```go
sort.Slice(candidates, func(i, j int) bool {
    return candidates[i].count > candidates[j].count
})
```

### Step 4: Verify Each Candidate

For each candidate anchor kernel:

1. **Find all positions** where the kernel appears
2. **Calculate cycle length** from the first two occurrences
3. **Verify consistency** - check that all occurrences are evenly spaced (5% tolerance)
4. **Verify the cycle content** using hash comparison (95% match threshold)
5. **Look for sub-cycles** if cycle length > 20

### Step 5: Group by Signature

Multiple anchors may detect the same pattern. We group by signature and keep the best:

```go
// Group by signature - keep the one with more repetitions
if existing, ok := signatureGroups[sig]; ok {
    if info.NumCycles > existing.Info.NumCycles {
        signatureGroups[sig] = &CyclePattern{...}
    }
} else {
    signatureGroups[sig] = &CyclePattern{...}
}
```

### Step 6: Calculate Temporal Position

For each pattern, we calculate:
- `StartPos`: First occurrence in trace
- `EndPos`: Last occurrence + cycle length
- `CenterPos`: Average of start and end (used for classification)

```go
startPos := info.StartIndex
endPos := info.CycleIndices[len(info.CycleIndices)-1] + info.CycleLength
centerPos := float64(startPos+endPos) / 2.0
```

---

## Cycle Verification: `verifyCycle`

Verifies that the detected pattern actually repeats consistently.

### Algorithm

1. **Hash all kernel names** for fast comparison
2. **Check each repetition** against the first cycle
3. **Require 95% match** for a valid repetition
4. **Return valid cycle** if at least 2 matches found

```go
// Require 95% match
if float64(matchCount)/float64(cycleLen) >= 0.95 {
    matches++
    cycleIndices = append(cycleIndices, pos)
}
```

---

## Sub-Cycle Detection: `findSubCycle`

Once an outer cycle is found, this function looks for smaller repeating patterns within it (e.g., transformer layers within a model pass).

### Algorithm

1. **Create kernel signatures** for pattern matching (not exact names)
2. **Find signatures that repeat** at regular intervals within the cycle
3. **Verify sub-cycle pattern** with 80% match threshold (more lenient)
4. **Calculate total repetitions** = sub-cycles per outer cycle × outer cycles

```go
// Require 80% signature match for sub-cycles
if float64(matchCount)/float64(cycleLen) >= 0.80 {
    matches++
}
```

---

## Kernel Signature Extraction: `getKernelSignature`

Creates simplified kernel names for pattern matching by removing:

1. **Template parameters** (content after `<`)
2. **Configuration suffixes** (`_GROUP_K_`, `_BLOCK_SIZE_`, etc.)
3. **Trailing numbers** (`_0`, `_1`, etc.)

### Example Transformations

| Input | Output |
|-------|--------|
| `void at::native::kernel<float, 4, true>` | `void at::native::kernel` |
| `triton_poi_fused_relu_0` | `triton_poi_fused_relu` |
| `ck_tile::kentry_GROUP_K_128` | `ck_tile::kentry` |

---

## Summary: Complete Detection Flow

```
Input: List of KernelEvent (name, timestamp, duration)
       PhaseMode: "auto" | "prefill" | "decode"

1. If PhaseMode is "prefill" or "decode":
   a. Find ALL valid cycles using anchor kernels
   b. For each anchor that produces a valid cycle:
      - Verify cycle consistency (5% gap tolerance)
      - Verify cycle content (95% hash match)
      - Look for sub-cycles if outer cycle > 20 kernels
      - Calculate temporal position (start, end, center)
      - Build signature for grouping
   
   c. Group similar patterns by signature
      - Keep pattern with most repetitions per signature
   
   d. Sort patterns by center position
   
   e. Select based on phase:
      - prefill: pattern with EARLIEST center
      - decode: pattern with LATEST center

2. If "auto" mode:
   - Use standard detection (most repetitions)

3. Return detected cycle with:
   - StartIndex: where first cycle begins
   - CycleLength: number of kernels per cycle
   - NumCycles: total repetitions found
   - CycleIndices: start position of each cycle
```

---

## Key Parameters and Thresholds

| Parameter | Value | Purpose |
|-----------|-------|---------|
| Minimum anchor count | 5 | Kernels must appear at least 5 times |
| Maximum anchor frequency | n/5 | Kernels can't appear more than 20% of total |
| Cycle length tolerance | 5% | Gap between occurrences can vary by 5% |
| Outer cycle match | 95% | Cycles must match 95% of kernel names |
| Sub-cycle match | 80% | Sub-cycles must match 80% of signatures |
| Minimum sub-cycle length | 5 | Sub-cycles must have at least 5 kernels |

---

## Why This Approach Works

### Problem with Fixed Boundaries

The previous approach used fixed percentages (first 15% for prefill, last 60% for decode). This fails when:
- Prefill is large and extends beyond 15%
- The prefill pattern appears frequently in the "decode region"

### Solution: Temporal Classification

By finding ALL patterns and classifying by center position:
- We don't assume where phases start/end
- Each pattern's temporal span is measured empirically
- The earliest pattern is prefill, latest is decode
- Works regardless of phase sizes or overlap
