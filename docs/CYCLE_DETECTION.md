# Cycle Detection Algorithm

Uplifter automatically detects repeating kernel patterns (cycles) in GPU traces. Each cycle typically represents a single transformer layer or similar repeating unit.

## Quick Start

```bash
# Extract all cycle patterns
./uplifter -input trace.json.gz -output analysis
# Creates: analysis_cycle_1.csv, analysis_cycle_2.csv, ...

# Compare two versions
./uplifter compare-all -baseline baseline -new optimized -output comparison.xlsx
```

---

## What It Detects

### Cycles (Layers)

Uplifter finds repeating kernel sequences in your trace:

```
Full trace:
[Layer1][Layer2][Layer3]...[LayerN] [Layer1][Layer2]... (repeated N times)

Detected cycle (single layer):
[Layer1] = [kernel_a, kernel_b, kernel_c, ...]
```

### Output

For each detected pattern, creates a CSV with:
- Kernel name
- Average duration (µs)
- Min/max duration
- Standard deviation
- Occurrence count
- Percentage of cycle time

---

## Detection Algorithm

The algorithm is implemented in `cycle.go` and works in stages.

### Stage 1: Find Anchor Candidates

```go
// Find kernels that repeat at consistent intervals
for name, count := range counts {
    if count >= 5 && count <= len(events)/5 {
        candidates = append(candidates, name)
    }
}
```

**Criteria:**
- Must appear at least 5 times
- Must appear at most N/5 times (not too frequent)

### Stage 2: Verify Cycle Consistency

For each candidate, check if it appears at regular intervals:

```go
positions := findKernelPositions(events, candidate)
cycleLen := positions[1] - positions[0]

// Allow 5% tolerance in cycle length
for i := 2; i < len(positions); i++ {
    diff := positions[i] - positions[i-1]
    if abs(diff - cycleLen) > cycleLen/20 {
        isConsistent = false
    }
}
```

### Stage 3: Verify Full Cycle Match

Verify that the kernel sequence actually repeats:

```go
for i := 1; i < expectedCycles; i++ {
    matchCount := 0
    for j := 0; j < cycleLen; j++ {
        if events[start+j].Name == events[start+i*cycleLen+j].Name {
            matchCount++
        }
    }
    // Require 95% match
    if matchCount/cycleLen >= 0.95 {
        matches++
    }
}
```

### Stage 4: Find Sub-Cycles

Within each outer cycle, look for smaller repeating patterns (layers within a full model pass):

```go
// Use kernel signatures for pattern matching
// Require 80% signature match for sub-cycles
```

---

## Kernel Signature Extraction

The `getKernelSignature()` function simplifies kernel names for pattern matching.

### Transformations

| Step | What's Removed | Example |
|------|----------------|---------|
| Template params | `<float, 128>` | `kernel<float>` → `kernel` |
| Config suffixes | `_GROUP_K_128` | Removed |
| Dimension suffixes | `_32x256` | Removed |
| Config prefixes | `_1tg_ps_` | Removed |
| Trailing numbers | `_0`, `_1` | Removed |

### Examples

| Original | Signature |
|----------|-----------|
| `gemm_kernel<float, 128>` | `gemm_kernel` |
| `aiter::fmoe_bf16_1tg_ps_32x256` | `aiter::fmoe_bf16` |
| `triton_poi_fused_add_0` | `triton_poi_fused_add` |

---

## Output Files

### CSV Format

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,kernel_a,50.5,45.2,55.8,2.3,1034,33.0
1,kernel_b,6.1,5.1,6.8,0.4,1034,4.0
```

| Column | Description |
|--------|-------------|
| `index` | Position within cycle |
| `kernel_name` | Full kernel name |
| `avg_duration_us` | Average duration (µs) |
| `min_duration_us` | Minimum duration |
| `max_duration_us` | Maximum duration |
| `stddev_us` | Standard deviation |
| `count` | Number of observations |
| `pct_of_cycle` | % of total cycle time |

---

## Thresholds

| Parameter | Value | Purpose |
|-----------|-------|---------|
| Min anchor occurrences | 5 | Filter rare kernels |
| Max anchor frequency | N/5 | Filter very common kernels |
| Cycle length tolerance | 5% | Allow slight variations |
| Outer cycle match | 95% | Strict matching |
| Sub-cycle match | 80% | More lenient for layers |
| Min cycle length | 10 | Avoid trivial patterns |

---

## Troubleshooting

### "No cycle patterns found"

- Trace may not have repeating patterns
- Try a longer trace with more iterations
- Check that the trace contains GPU kernel events

### Wrong pattern selected

- Uplifter outputs all detected patterns
- Compare the patterns you need using `compare-csv`

### Too many patterns detected

- Different kernel configurations create different patterns
- Use the one that best represents your workload
