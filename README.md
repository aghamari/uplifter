# Uplifter

A tool for analyzing and comparing Perfetto traces from AMD GPU deep learning workloads. Detects repeating kernel cycles (e.g., transformer layers) and compares different trace versions to identify performance changes.

## Features

- **Cycle Detection**: Automatically detects repeating kernel patterns in traces
- **Phase Detection**: Distinguishes between prefill and decode phases using boundary detection
- **Trace Comparison**: Compares baseline vs new traces, identifying:
  - Exact kernel matches
  - Similar kernels (same type, different parameters)
  - Fused/removed kernels (baseline kernels eliminated in new version)
  - New kernels (e.g., optimized implementations)
- **Performance Heatmap**: Change (%) column with color coding (green=faster, red=slower)
- **Statistics**: Min, max, avg, and stddev for each kernel
- **Multiple Output Formats**: CSV and Excel (.xlsx) with color-coded match types
- **Streaming Parser**: Efficiently handles large trace files (100MB+) with early stopping

## Installation

```bash
cd uplifter
go build -o uplifter .
```

Requires Go 1.21+

## Usage

### Single Trace Analysis

Extract kernel cycles from a trace:

```bash
# Basic usage (with early stopping for speed)
./uplifter -input trace.json.gz -output kernels.csv

# Full parse (slower but more accurate for complex traces)
./uplifter -input trace.json.gz -output kernels.csv -full

# Specify phase to detect
./uplifter -input trace.json.gz -output kernels.csv -full -phase decode
./uplifter -input trace.json.gz -output kernels.csv -full -phase prefill
```

### Trace Comparison

#### Two-Step Workflow (Recommended)

This is faster for iterative analysis - extract once, compare many times:

```bash
# Step 1: Extract cycles from each trace (~45s each, one-time)
./uplifter -input baseline.json.gz -output baseline.csv -full -phase decode
./uplifter -input new.json.gz -output new.csv -full -phase decode

# Step 2: Compare (instant - ~15ms)
./uplifter compare-csv -eager baseline.csv -compiled new.csv -output comparison.xlsx
```

#### Comparing Two Compiled Versions

When comparing two compiled/optimized traces (both have timing data):

```bash
# Extract both traces
./uplifter -input baseline_v1.json.gz -output baseline.csv -full -phase decode
./uplifter -input optimized_v2.json.gz -output new.csv -full -phase decode

# Compare - the -eager flag is for baseline, -compiled is for new version
./uplifter compare-csv -eager baseline.csv -compiled new.csv -output comparison.xlsx
```

#### Comparing Eager vs Compiled

When comparing eager mode (may lack timing) vs compiled mode:

```bash
./uplifter -input eager.json.gz -output eager.csv -full -phase decode
./uplifter -input compiled.json.gz -output compiled.csv -full -phase decode
./uplifter compare-csv -eager eager.csv -compiled compiled.csv -output comparison.xlsx
```

#### One-Step Workflow

For one-off comparisons (slower, parses both traces each time):

```bash
./uplifter compare -trace1 baseline.json.gz -trace2 new.json.gz -output comparison.xlsx -full -phase decode
```

## Phase Detection

The tool uses **boundary detection** to distinguish phases:

| Phase | Characteristic | Detection Method |
|-------|---------------|------------------|
| **Prefill** | Processes prompt, longer kernels | Searches first 15% of trace |
| **Decode** | Token generation, shorter kernels | Searches last 60% of trace |

```bash
-phase prefill  # Find cycle pattern in early part of trace
-phase decode   # Find cycle pattern in later part of trace (default)
-phase auto     # Same as decode
```

## Output Format

### CSV Output

Includes comprehensive statistics:

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,aiter::fmoe_bf16_blockscaleFp8...,50.499,3.519,61.360,18.892,93,33.02
1,void vllm::moe::topkGatingSoftmax...,6.064,5.119,6.799,0.408,93,3.97
```

### Excel Output (.xlsx)

**Columns:**
| Baseline Kernel | Base Avg | Base Min | Base Max | Base StdDev | New Kernel | New Avg | New Min | New Max | New StdDev | Change (%) | Match Type |

**Color Coding:**

Row colors (match type):
- 🟢 **Light Green**: Exact/similar matches
- 🟡 **Yellow**: New kernels (only in new version)
- 🔴 **Light Red**: Removed kernels (only in baseline)

Change (%) heatmap:
- 🟢 **Green** (< -5%): Performance improved (new is faster)
- 🟠 **Orange** (±5%): Similar performance
- 🔴 **Red** (> +5%): Performance regressed (new is slower)
- **"NEW"**: New kernel in optimized version
- **"REMOVED"**: Kernel eliminated (usually optimization)

## Command Reference

### `uplifter` (Cycle Detection)

| Flag | Description | Default |
|------|-------------|---------|
| `-input` | Input trace file (required) | - |
| `-output` | Output file (.csv, .json, .txt) | stdout |
| `-full` | Parse entire file (vs early stop) | false |
| `-phase` | Phase to detect: prefill/decode/auto | auto |
| `-min-cycle` | Minimum cycle length | 50 |
| `-max-cycle` | Maximum cycle length | 5000 |

### `uplifter compare`

| Flag | Description | Default |
|------|-------------|---------|
| `-trace1` | First trace (baseline) | - |
| `-trace2` | Second trace (new/optimized) | - |
| `-output` | Output file (.csv or .xlsx) | stdout |
| `-full` | Full parse mode | false |
| `-phase` | Phase to detect | decode |

### `uplifter compare-csv`

| Flag | Description | Default |
|------|-------------|---------|
| `-eager` | Baseline CSV (from uplifter) | - |
| `-compiled` | New/optimized CSV (from uplifter) | - |
| `-output` | Output file (.csv or .xlsx) | stdout |

## Supported Trace Formats

- Perfetto JSON traces (`.json`)
- Gzipped traces (`.json.gz`)

## Example: Full Analysis Workflow

```bash
# 1. Extract decode cycles from baseline and new traces
./uplifter -input baseline.json.gz -output baseline_decode.csv -full -phase decode
./uplifter -input optimized.json.gz -output new_decode.csv -full -phase decode

# 2. Extract prefill cycles
./uplifter -input baseline.json.gz -output baseline_prefill.csv -full -phase prefill
./uplifter -input optimized.json.gz -output new_prefill.csv -full -phase prefill

# 3. Compare decode phase
./uplifter compare-csv -eager baseline_decode.csv -compiled new_decode.csv -output decode_comparison.xlsx

# 4. Compare prefill phase
./uplifter compare-csv -eager baseline_prefill.csv -compiled new_prefill.csv -output prefill_comparison.xlsx

# 5. Open .xlsx files in Excel to see color-coded performance changes
```

## Performance Tips

- Use `-full` flag for accurate cycle detection on complex traces
- Use two-step workflow (extract CSVs first) for iterative comparisons
- Early stopping (`-full=false`) is faster but may miss cycles in long traces

## License

MIT
