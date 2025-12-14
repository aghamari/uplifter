# Uplifter Cycle Detection

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

Uplifter automatically separates these by:
1. Finding ALL repeating kernel patterns in the trace
2. Filtering to significant patterns (>1% of total events)
3. Selecting prefill as the pattern with the **earliest** temporal position
4. Selecting decode as the pattern with the **latest** temporal position

### Sub-Cycles (Layers)

Within each phase, uplifter finds the smallest repeating unit - typically a single transformer layer:

```
Outer Cycle (full model pass):
[Layer1][Layer2][Layer3]...[LayerN] [Layer1][Layer2][Layer3]...[LayerN] ...

Sub-Cycle (single layer):
[Layer1] [Layer1] [Layer1] ...
```

## Output Files

### CSV Format

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,kernel_a,50.5,45.2,55.8,2.3,1034,33.0
1,kernel_b,6.1,5.1,6.8,0.4,1034,4.0
...
```

| Column | Description |
|--------|-------------|
| `index` | Position within the cycle (0-based) |
| `kernel_name` | Full kernel name |
| `avg_duration_us` | Average duration in microseconds |
| `min_duration_us` | Minimum duration |
| `max_duration_us` | Maximum duration |
| `stddev_us` | Standard deviation |
| `count` | Number of times this kernel was observed |
| `pct_of_cycle` | Percentage of total cycle time |

### XLSX Comparison Format

When comparing two traces, the XLSX includes:

**Columns:**
- Baseline Kernel, Base Avg/Min/Max/StdDev
- New Kernel, New Avg/Min/Max/StdDev  
- Change (%) - with heatmap coloring
- Match Type

**Match Types:**
| Type | Meaning | Color |
|------|---------|-------|
| `exact` | Identical kernel names | Light Green |
| `similar` | Same kernel type, different config (e.g., block sizes) | Light Blue |
| `removed` | Only in baseline (optimized away) | Light Red |
| `new_only` | Only in new version (new optimization) | Light Yellow |

**Change (%) Heatmap:**
- 🟢 Green: Improved (>5% faster)
- 🟠 Orange: Neutral (within ±5%)
- 🔴 Red: Regressed (>5% slower)

## Example Workflow

```bash
# 1. Extract cycles from baseline trace
./uplifter -input baseline.json.gz -output baseline
# Creates: baseline_prefill.csv, baseline_decode.csv

# 2. Extract cycles from optimized trace  
./uplifter -input optimized.json.gz -output optimized
# Creates: optimized_prefill.csv, optimized_decode.csv

# 3. Compare decode phases
./uplifter compare-csv \
  -baseline baseline_decode.csv \
  -new optimized_decode.csv \
  -output decode_comparison.xlsx

# 4. Compare prefill phases
./uplifter compare-csv \
  -baseline baseline_prefill.csv \
  -new optimized_prefill.csv \
  -output prefill_comparison.xlsx
```

## How Pattern Detection Works

1. **Count kernel occurrences** - Find kernels that appear multiple times
2. **Find anchor kernels** - Kernels that appear at regular intervals
3. **Verify cycles** - Confirm the pattern repeats consistently (95% match)
4. **Find sub-cycles** - Look for smaller patterns within outer cycles (80% match)
5. **Calculate positions** - Determine where each pattern occurs in the trace
6. **Filter by significance** - Keep patterns covering >1% of total events
7. **Classify phases** - Earliest significant pattern = prefill, latest = decode

## Troubleshooting

### "No cycle patterns found"
- The trace may not have repeating patterns
- Try a longer trace with more iterations

### Prefill and decode look the same
- The workload may not have distinct phases
- Check if ISL (input sequence length) and OSL (output sequence length) differ

### Wrong pattern selected
- Uplifter selects based on temporal position and significance
- Patterns must cover >1% of trace events to be considered
