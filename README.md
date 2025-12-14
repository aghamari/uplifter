# Uplifter

A tool for analyzing and comparing Perfetto traces from AMD GPU deep learning workloads. Automatically detects repeating kernel cycles (transformer layers) and compares different trace versions.

## Features

- **Automatic Cycle Detection**: Finds all repeating kernel patterns in traces
- **Phase Detection**: Separates prefill and decode phases (LLM mode)
- **Trace Comparison**: Two matching modes with automatic rotation detection
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
# LLM mode (default) - detects prefill and decode phases
./uplifter -input trace.json.gz -output analysis
# Creates: analysis_prefill.csv, analysis_decode.csv

# All mode - outputs all detected cycle patterns
./uplifter -input trace.json.gz -output analysis -mode all
# Creates: analysis_cycle_1.csv, analysis_cycle_2.csv, ...
```

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
./uplifter -input <trace.json.gz> -output <basename> [-mode llm|all]
```

| Flag | Description |
|------|-------------|
| `-input` | Path to Perfetto trace file (.json or .json.gz) |
| `-output` | Output base path for CSV files |
| `-mode` | Detection mode: `llm` (default) or `all` |

#### Detection Modes

| Mode | Output | Use Case |
|------|--------|----------|
| `llm` | `_prefill.csv`, `_decode.csv` | LLM inference traces |
| `all` | `_cycle_1.csv`, `_cycle_2.csv`, ... | General traces, multiple patterns |

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
| `match` | Compiled vs Compiled | Signature-based matching (position-independent) |
| `align` | Eager vs Compiled | LCS alignment with automatic rotation detection |

The `align` mode automatically detects cycle rotation when comparing same-length cycles, ensuring optimal alignment even when cycle detection started at different anchor kernels.

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

### Analyzing All Patterns in a Trace

```bash
# Detect all cycle patterns
./uplifter -input trace.json.gz -output analysis -mode all
# Creates: analysis_cycle_1.csv, analysis_cycle_2.csv, ...

# Compare any two patterns
./uplifter compare-csv \
  -baseline analysis_cycle_1.csv \
  -new analysis_cycle_3.csv \
  -mode align \
  -output pattern_comparison.xlsx
```

### Comparing Two Compiled Versions

```bash
# Analyze both versions
./uplifter -input baseline.json.gz -output baseline
./uplifter -input optimized.json.gz -output optimized

# Compare decode phases
./uplifter compare-csv \
  -baseline baseline_decode.csv \
  -new optimized_decode.csv \
  -output decode_comparison.xlsx
```

### Comparing Eager vs Compiled

```bash
# Analyze both modes
./uplifter -input eager.json.gz -output eager
./uplifter -input compiled.json.gz -output compiled

# Compare using align mode
./uplifter compare-csv \
  -baseline eager_decode.csv \
  -new compiled_decode.csv \
  -mode align \
  -output eager_vs_compiled.xlsx
```

## Documentation

- **[Cycle Detection](docs/CYCLE_DETECTION.md)** - How cycle and phase detection works
- **[Matching Algorithms](docs/MATCHING_ALGORITHMS.md)** - Kernel matching, signatures, and rotation detection

## License

MIT
