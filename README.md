# Uplifter

A tool for analyzing and comparing Perfetto traces from AMD GPU workloads. Automatically detects repeating kernel cycles and compares trace versions to identify performance changes.

## Features

- **Automatic Cycle Detection**: Finds all repeating kernel patterns in traces
- **Trace Comparison**: LCS-based alignment with automatic rotation detection
- **Performance Heatmap**: Color-coded XLSX output showing speedups/regressions
- **Statistics**: Min, max, avg, and stddev for each kernel

## Installation

```bash
cd uplifter
go build -o uplifter .
```

Requires Go 1.21+

## Quick Start

### 1. Extract Cycles from a Trace

```bash
./uplifter -input trace.json.gz -output analysis
# Creates: analysis_cycle_1.csv, analysis_cycle_2.csv, ...
```

### 2. Compare Two Traces

```bash
# Extract from both traces
./uplifter -input baseline.json.gz -output baseline
./uplifter -input optimized.json.gz -output optimized

# Compare single cycle
./uplifter compare-csv \
  -baseline baseline_cycle_1.csv \
  -new optimized_cycle_1.csv \
  -output comparison.xlsx

# Compare all cycles at once
./uplifter compare-all \
  -baseline baseline \
  -new optimized \
  -output all_comparisons.xlsx
```

## Commands

### `uplifter` - Cycle Detection

```bash
./uplifter -input <trace.json.gz> -output <basename>
```

| Flag | Description |
|------|-------------|
| `-input` | Path to Perfetto trace file (.json or .json.gz) |
| `-output` | Output base path for CSV files |

**Output:** Creates `_cycle_1.csv`, `_cycle_2.csv`, etc. for each detected pattern.

### `uplifter compare-csv` - Compare Two Cycles

```bash
./uplifter compare-csv -baseline <file.csv> -new <file.csv> -output <file.xlsx>
```

| Flag | Description |
|------|-------------|
| `-baseline` | Path to baseline CSV |
| `-new` | Path to new/optimized CSV |
| `-output` | Output file (.csv or .xlsx) |
| `-mode` | `align` (default) or `match` |

### `uplifter compare-all` - Compare All Cycles

```bash
./uplifter compare-all -baseline <base_path> -new <base_path> -output <file.xlsx>
```

Compares all matching `_cycle_N.csv` files and creates a single XLSX with tabs for each cycle.

| Flag | Description |
|------|-------------|
| `-baseline` | Base path for baseline CSVs (e.g., `baseline` finds `baseline_cycle_1.csv`, etc.) |
| `-new` | Base path for new CSVs |
| `-output` | Output XLSX file with multiple tabs |

## Output Formats

### CSV Output

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,kernel_a,50.5,45.2,55.8,2.3,1034,33.0
1,kernel_b,6.1,5.1,6.8,0.4,1034,4.0
```

### XLSX Comparison

Color-coded Excel file with:
- **Light Green**: Exact matches (identical kernel names)
- **Light Blue**: Similar matches (same kernel type, different config)
- **Light Yellow**: New kernels (only in new version)
- **Light Red**: Removed kernels (only in baseline)

Change (%) heatmap:
- 🟢 **Green**: Faster (improvement >5%)
- 🟠 **Orange**: Similar (within ±5%)
- 🔴 **Red**: Slower (regression >5%)

## Example Workflows

### Comparing Two Trace Versions

```bash
# Extract cycles from both
./uplifter -input baseline.json.gz -output baseline
./uplifter -input optimized.json.gz -output optimized

# Compare all cycles
./uplifter compare-all \
  -baseline baseline \
  -new optimized \
  -output comparison.xlsx
```

### Comparing Eager vs Compiled

```bash
# Extract cycles
./uplifter -input eager.json.gz -output eager
./uplifter -input compiled.json.gz -output compiled

# Compare (align mode handles kernel fusion)
./uplifter compare-csv \
  -baseline eager_cycle_1.csv \
  -new compiled_cycle_1.csv \
  -output eager_vs_compiled.xlsx
```

## Comparison Modes

| Mode | Best For | Algorithm |
|------|----------|-----------|
| `align` (default) | Most cases | LCS alignment with rotation detection |
| `match` | Heavily reordered | Signature-based greedy matching |

The default `align` mode automatically detects cycle rotation, ensuring optimal alignment even when cycle detection started at different anchor kernels.

## Documentation

- **[Cycle Detection](docs/CYCLE_DETECTION.md)** - How cycle detection works
- **[Matching Algorithms](docs/MATCHING_ALGORITHMS.md)** - Kernel matching and rotation detection

## License

MIT
