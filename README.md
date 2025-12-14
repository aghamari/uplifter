# Uplifter

A tool for analyzing and comparing Perfetto traces from AMD GPU deep learning workloads. Automatically detects repeating kernel cycles (transformer layers) and compares different trace versions.

## Features

- **Automatic Phase Detection**: Separates prefill and decode phases automatically
- **Cycle Detection**: Finds repeating kernel patterns (transformer layers)
- **Trace Comparison**: Compares baseline vs optimized traces with two matching modes
- **Performance Heatmap**: Color-coded performance changes in XLSX output
- **Statistics**: Min, max, avg, and stddev for each kernel

## Installation

```bash
cd uplifter
go build -o uplifter .
```

Requires Go 1.21+

## Quick Start

### 1. Analyze a Trace

```bash
./uplifter -input trace.json.gz -output analysis
```

This creates two files:
- `analysis_prefill.csv` - Prefill phase kernels
- `analysis_decode.csv` - Decode phase kernels

### 2. Compare Two Traces

```bash
# Extract from both traces
./uplifter -input baseline.json.gz -output baseline
./uplifter -input optimized.json.gz -output optimized

# Compare decode phases (compiled vs compiled)
./uplifter compare-csv \
  -baseline baseline_decode.csv \
  -new optimized_decode.csv \
  -output decode_comparison.xlsx

# Compare eager vs compiled (use align mode)
./uplifter compare-csv \
  -baseline eager_decode.csv \
  -new compiled_decode.csv \
  -mode align \
  -output eager_vs_compiled.xlsx
```

## Commands

### `uplifter` - Cycle Detection

```bash
./uplifter -input <trace.json.gz> -output <basename>
```

| Flag | Description |
|------|-------------|
| `-input` | Path to Perfetto trace file (.json or .json.gz) |
| `-output` | Output base path (creates _prefill.csv and _decode.csv) |

### `uplifter compare-csv` - Compare Traces

```bash
./uplifter compare-csv -baseline <file.csv> -new <file.csv> [options]
```

| Flag | Description |
|------|-------------|
| `-baseline` | Path to baseline CSV |
| `-new` | Path to new/optimized CSV |
| `-output` | Output file (.csv or .xlsx) |
| `-mode` | Comparison mode: `match` (default) or `align` |

#### Comparison Modes

| Mode | Best For | Algorithm |
|------|----------|-----------|
| `match` | Compiled vs Compiled | Signature-based matching - finds best matches regardless of position |
| `align` | Eager vs Compiled | LCS position-based alignment - shows insertions/deletions in order |

Use `-mode align` when comparing eager mode traces against compiled mode traces, as kernel order differs significantly. Use the default `-mode match` when comparing two compiled traces where kernels may have moved positions but should still match.

## Output Formats

### CSV Output

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,kernel_a,50.5,45.2,55.8,2.3,1034,33.0
1,kernel_b,6.1,5.1,6.8,0.4,1034,4.0
```

### XLSX Comparison

Color-coded Excel file with:
- **Light Green rows**: Exact matches (identical kernel names)
- **Light Blue rows**: Similar matches (same kernel, different config/block sizes)
- **Light Yellow rows**: New kernels (only in optimized version)
- **Light Red rows**: Removed kernels (only in baseline)

Change (%) heatmap:
- 🟢 **Green**: Faster (improvement >5%)
- 🟠 **Orange**: Similar (within ±5%)
- 🔴 **Red**: Slower (regression >5%)

## Example Workflows

### Comparing Two Compiled Versions

```bash
# Analyze baseline
./uplifter -input baseline.json.gz -output baseline

# Analyze optimized version
./uplifter -input optimized.json.gz -output optimized

# Compare decode (main workload) - uses match mode by default
./uplifter compare-csv \
  -baseline baseline_decode.csv \
  -new optimized_decode.csv \
  -output decode_comparison.xlsx
```

### Comparing Eager vs Compiled

```bash
# Analyze eager mode trace
./uplifter -input eager.json.gz -output eager

# Analyze compiled mode trace
./uplifter -input compiled.json.gz -output compiled

# Compare using align mode (preserves execution order)
./uplifter compare-csv \
  -baseline eager_decode.csv \
  -new compiled_decode.csv \
  -mode align \
  -output eager_vs_compiled.xlsx
```

## Documentation

- **[Cycle Detection](docs/CYCLE_DETECTION.md)** - How cycle and phase detection works
- **[Matching Algorithms](docs/MATCHING_ALGORITHMS.md)** - Kernel matching and signature extraction

## License

MIT
