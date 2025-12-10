# Uplifter

A tool for analyzing and comparing Perfetto traces from AMD GPU deep learning workloads. Detects repeating kernel cycles (e.g., transformer layers) and compares eager vs compiled mode traces.

## Features

- **Cycle Detection**: Automatically detects repeating kernel patterns in traces
- **Phase Detection**: Distinguishes between prefill and decode phases using repetition-based heuristics
- **Trace Comparison**: Compares eager vs compiled mode traces, identifying:
  - Exact kernel matches
  - Similar kernels (same type, different parameters)
  - Fused/removed kernels (eager kernels eliminated in compiled mode)
  - New compiled-only kernels (e.g., Triton fused ops)
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
uplifter -input trace.json.gz -output kernels.csv

# Full parse (slower but more accurate for complex traces)
uplifter -input trace.json.gz -output kernels.csv -full

# Specify phase to detect
uplifter -input trace.json.gz -output kernels.csv -full -phase decode
uplifter -input trace.json.gz -output kernels.csv -full -phase prefill
```

### Trace Comparison

#### Two-Step Workflow (Recommended for iteration)

```bash
# Step 1: Extract cycles from each trace
uplifter -input eager.json.gz -output eager.csv -full -phase decode
uplifter -input compiled.json.gz -output compiled.csv -full -phase decode

# Step 2: Compare (instant - uses cached CSVs)
uplifter compare-csv -eager eager.csv -compiled compiled.csv -output comparison.csv
uplifter compare-csv -eager eager.csv -compiled compiled.csv -output comparison.xlsx
```

#### One-Step Workflow

```bash
uplifter compare -trace1 eager.json.gz -trace2 compiled.json.gz -output comparison.csv
```

## Phase Detection

The tool uses **repetition count** for phase detection (model-agnostic):

| Phase | Characteristic | Detection |
|-------|---------------|-----------|
| **Prefill** | Processes prompt once | Fewer repetitions |
| **Decode** | Generates tokens one-by-one | Many repetitions |

```bash
-phase prefill  # Find cycle with FEWER repetitions
-phase decode   # Find cycle with MOST repetitions (default)
-phase auto     # Same as decode
```

## Output Format

### CSV Output

```csv
eager_kernel,compiled_kernel,duration_us,match_type
aiter::fmoe_bf16...,aiter::fmoe_bf16...,136.04,exact
(none),triton_poi_fused_index_put_0,4.14,compiled_only
void at::native::elementwise_kernel...,.,fused
```

### Excel Output (.xlsx)

Color-coded rows:
- 🟢 **Green**: Exact matches
- 🟡 **Yellow**: Compiled-only (new fused kernels)
- 🔴 **Red**: Fused/removed (eager kernels eliminated)

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
| `-trace1` | First trace (eager mode) | - |
| `-trace2` | Second trace (compiled mode) | - |
| `-output` | Output file (.csv or .xlsx) | stdout |
| `-full` | Full parse mode | false |

### `uplifter compare-csv`

| Flag | Description | Default |
|------|-------------|---------|
| `-eager` | Eager mode CSV (from uplifter) | - |
| `-compiled` | Compiled mode CSV (from uplifter) | - |
| `-output` | Output file (.csv or .xlsx) | stdout |

## Supported Trace Formats

- Perfetto JSON traces (`.json`)
- Gzipped traces (`.json.gz`)

## Example Workflow

```bash
# 1. Analyze a trace
./uplifter -input /path/to/trace.json.gz -output analysis.csv -full -phase decode

# 2. Compare eager vs compiled
./uplifter -input eager.json.gz -output eager.csv -full -phase decode
./uplifter -input compiled.json.gz -output compiled.csv -full -phase decode
./uplifter compare-csv -eager eager.csv -compiled compiled.csv -output comparison.xlsx

# 3. Open comparison.xlsx in Excel to see color-coded results
```

## License

MIT

