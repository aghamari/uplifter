# Uplifter

A tool for analyzing and comparing Perfetto traces from AMD GPU deep learning workloads. Automatically detects repeating kernel cycles (transformer layers) and compares different trace versions.

## Features

- **Automatic Phase Detection**: Separates prefill and decode phases automatically
- **Cycle Detection**: Finds repeating kernel patterns (transformer layers)
- **Trace Comparison**: Compares baseline vs optimized traces
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

# Compare decode phases
./uplifter compare-csv \
  -baseline baseline_decode.csv \
  -new optimized_decode.csv \
  -output decode_comparison.xlsx
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
./uplifter compare-csv -baseline <file.csv> -new <file.csv> -output <file.xlsx>
```

| Flag | Description |
|------|-------------|
| `-baseline` | Path to baseline CSV |
| `-new` | Path to new/optimized CSV |
| `-output` | Output file (.csv or .xlsx) |

## Output Formats

### CSV Output

```csv
index,kernel_name,avg_duration_us,min_duration_us,max_duration_us,stddev_us,count,pct_of_cycle
0,kernel_a,50.5,45.2,55.8,2.3,1034,33.0
1,kernel_b,6.1,5.1,6.8,0.4,1034,4.0
```

### XLSX Comparison

Color-coded Excel file with:
- **Green rows**: Matched kernels (exact or similar)
- **Yellow rows**: New kernels (only in optimized version)
- **Red rows**: Removed kernels (only in baseline)

Change (%) heatmap:
- 🟢 **Green**: Faster (improvement >5%)
- 🟠 **Orange**: Similar (within ±5%)
- 🔴 **Red**: Slower (regression >5%)

## Example: Full Analysis

```bash
# Analyze baseline
./uplifter -input baseline.json.gz -output baseline
# Creates: baseline_prefill.csv, baseline_decode.csv

# Analyze optimized version
./uplifter -input optimized.json.gz -output optimized
# Creates: optimized_prefill.csv, optimized_decode.csv

# Compare decode (main workload)
./uplifter compare-csv \
  -baseline baseline_decode.csv \
  -new optimized_decode.csv \
  -output decode_comparison.xlsx

# Compare prefill
./uplifter compare-csv \
  -baseline baseline_prefill.csv \
  -new optimized_prefill.csv \
  -output prefill_comparison.xlsx

# Open XLSX files in Excel to see results
```

## Documentation

See [docs/CYCLE_DETECTION.md](docs/CYCLE_DETECTION.md) for detailed information about:
- How cycle detection works
- Prefill vs decode phase separation
- Output file formats
- Troubleshooting

## License

MIT
