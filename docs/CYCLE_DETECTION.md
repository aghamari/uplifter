# Cycle Detection Algorithm

Uplifter automatically detects repeating kernel patterns (cycles) in GPU traces and separates them into **prefill** and **decode** phases.

## Quick Start

```bash
# Analyze a trace - creates both prefill and decode CSVs
./uplifter -input trace.json.gz -output analysis
# Creates: analysis_prefill.csv, analysis_decode.csv

# Compare two versions
./uplifter compare-csv -baseline baseline_decode.csv -new optimized_decode.csv -output comparison.xlsx
```

## What It Detects

### Prefill vs Decode

In LLM inference, there are two distinct phases:

| Phase | Description | Characteristics |
|-------|-------------|-----------------|
| **Prefill** | Processing the input prompt | Happens at the beginning, larger kernels, fewer cycles |
| **Decode** | Generating output tokens | Happens at the end, smaller kernels, many cycles |

### Sub-Cycles (Layers)

Within each phase, uplifter finds the smallest repeating unit - typically a single transformer layer:

```
Outer Cycle (full model pass):
[Layer1][Layer2][Layer3]...[LayerN] [Layer1][Layer2][Layer3]...[LayerN] ...

Sub-Cycle (single layer):
[Layer1] [Layer1] [Layer1] ...
```

---

## Detection Algorithm

The algorithm is implemented in `cycle.go` and consists of several stages.

### Stage 1: Find Anchor Candidates

```go
// From findAllCyclePatterns() and findOuterCycle()
counts := make(map[string]int)
for _, e := range events {
    counts[e.Name]++
}

// Find kernels that appear multiple times but not too frequently
for name, count := range counts {
    if count >= 5 && count <= len(events)/5 {
        estimatedCycleLen := len(events) / count
        candidates = append(candidates, candidate{name, count, estimatedCycleLen})
    }
}
```

**Criteria:**
- Kernel must appear at least 5 times
- Kernel must appear at most N/5 times (where N = total events)
- This filters out both rare kernels and very frequent ones

### Stage 2: Verify Cycle Consistency

For each candidate anchor kernel, check if it appears at regular intervals:

```go
// From findOuterCycle()
positions := findKernelPositions(events, cand.name)
cycleLen := positions[1] - positions[0]

isConsistent := true
for i := 2; i < len(positions); i++ {
    diff := positions[i] - positions[i-1]
    // Allow 5% tolerance
    if abs(diff - cycleLen) > max(1, cycleLen/20) {
        isConsistent = false
        break
    }
}
```

**Tolerance:** 5% deviation allowed (cycleLen/20)

### Stage 3: Verify Full Cycle Match

Verify that the full kernel sequence repeats:

```go
// From verifyCycle()
for i := 1; i < expectedCycles; i++ {
    pos := startIdx + i*cycleLen
    
    matchCount := 0
    for j := 0; j < cycleLen; j++ {
        if hashes[startIdx+j] == hashes[pos+j] {
            matchCount++
        }
    }
    
    // Require 95% match
    if float64(matchCount)/float64(cycleLen) >= 0.95 {
        matches++
    }
}
```

**Match threshold:** 95% of kernels must match exactly

### Stage 4: Find Sub-Cycles

Within each outer cycle, look for smaller repeating patterns:

```go
// From findSubCycle()
// Create type signatures for pattern matching
signatures := make([]string, n)
for i, e := range cycleEvents {
    signatures[i] = getKernelSignature(e.Name)
}

// Look for signatures that repeat at regular intervals
// Require 80% signature match for sub-cycles
if float64(matchCount)/float64(cycleLen) >= 0.80 {
    matches++
}
```

**Sub-cycle threshold:** 80% signature match (more lenient than outer cycles)

---

## Phase Detection

Phase detection uses the `detectPhaseByAllCycles()` function.

### Algorithm

1. **Find ALL distinct cycle patterns** in the trace
2. **Group by signature** (kernel composition)
3. **Calculate temporal center** for each pattern
4. **Classify by position:**
   - Earliest center position → **Prefill**
   - Latest center position → **Decode**

```go
// From detectPhaseByAllCycles()
patterns := findAllCyclePatterns(events)

// Sort patterns by center position (earlier first)
sort.Slice(patterns, func(i, j int) bool {
    return patterns[i].CenterPos < patterns[j].CenterPos
})

if phase == "prefill" {
    return patterns[0].Info  // Earliest
} else {
    return patterns[len(patterns)-1].Info  // Latest
}
```

### Temporal Position Calculation

```go
// Center position = average of start and end
startPos := info.StartIndex
endPos := info.CycleIndices[len(info.CycleIndices)-1] + info.CycleLength
centerPos := float64(startPos + endPos) / 2.0
```

### Cycle Signature

Cycles are grouped by their kernel composition:

```go
// From getCycleSignature()
// Build signature from first 10 kernel types in the cycle
var sigs []string
for i := 0; i < min(cycle.CycleLength, 10); i++ {
    idx := cycle.StartIndex + i
    sig := getKernelSignature(events[idx].Name)
    sigs = append(sigs, sig)
}
return strings.Join(sigs, "|")
```

---

## Kernel Signature Extraction

The `getKernelSignature()` function simplifies kernel names for pattern matching.

### Transformations Applied

| Step | What's Removed | Example |
|------|----------------|---------|
| 1 | Template parameters | `kernel<float>` → `kernel` |
| 2 | Config suffixes | `_GROUP_K_128` → removed |
| 3 | Dimension suffixes | `_32x256` → removed |
| 4 | Config prefixes | `_1tg_ps_` → removed |
| 5 | Trailing numbers | `_0`, `_1` → removed |

### Config Patterns Removed

```go
configPatterns := []string{
    "_GROUP_K_", "_GROUP_N_", "_GROUP_SIZE_",
    "_BLOCK_SIZE_", "_SPLITK_BLOCK_SIZE_",
    "_NUM_KSPLIT_", "_ACTUAL_KSPLIT_", "_MAX_KSPLIT_",
    "_GRID_MN_", "_GRID_",
    "_EVEN_K_", "_cache_modifier_",
}

configSuffixes := []string{"_1tg_ps", "_1tg", "_ps", "_novs", "_vs"}
```

### Dimension Detection

```go
// isDimensionSuffix checks for NxM pattern (e.g., "32x256")
func isDimensionSuffix(s string) bool {
    xIdx := strings.Index(s, "x")
    // Verify digits before and after 'x'
    for i := 0; i < xIdx; i++ {
        if s[i] < '0' || s[i] > '9' { return false }
    }
    for i := xIdx + 1; i < len(s); i++ {
        if s[i] < '0' || s[i] > '9' { return false }
    }
    return true
}
```

---

## Output Files

### CSV Format

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,kernel_a,50.5,45.2,55.8,2.3,1034,33.0
1,kernel_b,6.1,5.1,6.8,0.4,1034,4.0
```

| Column | Description | Source |
|--------|-------------|--------|
| `index` | Position within cycle | `KernelStats.IndexInCycle` |
| `kernel_name` | Full kernel name | `KernelStats.Name` |
| `avg_duration_us` | Average duration (µs) | `KernelStats.AvgDur` |
| `min_duration_us` | Minimum duration | `KernelStats.MinDur` |
| `max_duration_us` | Maximum duration | `KernelStats.MaxDur` |
| `stddev_us` | Standard deviation | `KernelStats.StdDev` |
| `count` | Number of observations | `KernelStats.Count` |
| `pct_of_cycle` | % of total cycle time | Calculated from `AvgDur` |

---

## Thresholds Summary

| Parameter | Value | Location |
|-----------|-------|----------|
| Min anchor occurrences | 5 | `findOuterCycle()` |
| Max anchor frequency | N/5 | `findOuterCycle()` |
| Cycle length tolerance | 5% | `findOuterCycle()` |
| Outer cycle match | 95% | `verifyCycle()` |
| Sub-cycle match | 80% | `verifySubCyclePattern()` |
| Min cycle length | 10 | `findAllCyclePatterns()` |
| Min sub-cycle reps | 3 | `findSubCycle()` |

---

## Troubleshooting

### "No cycle patterns found"

- The trace may not have enough repeating patterns
- Try a longer trace with more iterations
- Check that the trace contains GPU kernel events

### Prefill and decode look the same

- The workload may not have distinct phases
- ISL (input sequence length) and OSL (output sequence length) might be similar
- The model might have uniform behavior across phases

### Wrong pattern selected

- Uplifter selects based on temporal position
- Prefill = earliest significant pattern
- Decode = latest significant pattern
- If patterns overlap significantly, classification may be imperfect

### Too many/few kernels in cycle

- Adjust the outer/sub-cycle selection manually if needed
- Check the cycle signature in debug output
- The algorithm prioritizes patterns with most repetitions
