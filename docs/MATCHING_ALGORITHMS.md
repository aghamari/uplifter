# Kernel Matching Algorithms

Uplifter provides two matching algorithms for comparing kernel cycles. The default `align` mode works for most cases.

## Quick Reference

| Mode | Best For | Command |
|------|----------|---------|
| `align` (default) | Most cases | `compare-csv -baseline a.csv -new b.csv` |
| `match` | Heavily reordered | `compare-csv -baseline a.csv -new b.csv -mode match` |

---

## Mode 1: Alignment (`-mode align`, default)

Uses LCS (Longest Common Subsequence) with automatic rotation detection.

### How It Works

1. **Detect rotation**: Try all rotations of baseline to find best alignment
2. **Build signatures**: Simplify kernel names for matching
3. **Compute LCS**: Find longest matching subsequence
4. **Backtrack**: Produce aligned output showing matches, insertions, deletions

### Rotation Detection

Cycles may start at different positions depending on which anchor kernel was found first. The algorithm automatically corrects this:

```go
// Double baseline to handle wrap-around
doubledSigs := append(eagerSigs, eagerSigs...)

for rot := 0; rot < len(eager); rot++ {
    windowSigs := doubledSigs[rot : rot+len(eager)]
    lcs := computeLCS(windowSigs, compiledSigs)
    if lcs > bestLCS {
        bestLCS = lcs
        bestRotation = rot
    }
}
```

**Output:** Interleaved in execution order:
- Matched kernels side-by-side
- Removed kernels with "." in new column
- New kernels with empty baseline column

### When to Use

- Comparing eager vs compiled
- Comparing different trace versions
- When execution order is meaningful
- When you want to see where kernels were added/removed

---

## Mode 2: Signature Matching (`-mode match`)

Uses greedy signature-based matching, ignoring position.

### How It Works

1. **Build maps** of all baseline kernels by name and signature
2. **For each new kernel**, find best match:
   - Exact name match first
   - Then signature match
3. **Mark unmatched** baseline kernels as "removed"

### When to Use

- Traces with heavy kernel reordering
- When position doesn't matter
- When you just want to match equivalent kernels

---

## Kernel Signatures

Both algorithms use signatures to match similar kernels.

### What Gets Removed

| Pattern | Examples |
|---------|----------|
| Template params | `<float, 128>` |
| Config suffixes | `_GROUP_K_128`, `_BLOCK_SIZE_256` |
| Dimension suffixes | `_32x256`, `_128x64` |
| Config prefixes | `_1tg_ps`, `_vs` |
| Trailing numbers | `_0`, `_1` |

### Examples

| Original | Signature |
|----------|-----------|
| `gemm_kernel<float, 128>` | `gemm_kernel` |
| `aiter::fmoe_bf16_1tg_ps_32x256` | `aiter::fmoe_bf16` |
| `ck_tile::kentry_GROUP_K_128` | `ck_tile::kentry` |

---

## Match Types

| Type | Meaning | XLSX Color |
|------|---------|------------|
| `exact` | Identical kernel names | Light Green |
| `similar` | Same signature, different config | Light Blue |
| `new_only` | Only in new trace | Light Yellow |
| `removed` | Only in baseline | Light Red |

---

## Performance Heatmap

The "Change (%)" column shows:

| Change | Color | Meaning |
|--------|-------|---------|
| < -5% | Green | Faster |
| ±5% | Orange | Similar |
| > +5% | Red | Slower |

Special values:
- `NEW` - Only in new trace
- `REMOVED` - Only in baseline
