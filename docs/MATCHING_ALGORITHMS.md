# Kernel Matching Algorithms

Uplifter provides two matching algorithms for comparing kernel cycles between traces. The choice depends on what you're comparing.

## Quick Reference

| Comparison Type | Mode | Command |
|-----------------|------|---------|
| General use | `align` (default) | `compare-csv -baseline a.csv -new b.csv` |
| Heavily reordered | `match` | `compare-csv -baseline a.csv -new b.csv -mode match` |

---

## Mode 1: Signature-Based Matching (`-mode match`)

Best for comparing traces where kernels have been heavily reordered and position-based alignment fails.

### How It Works

1. **Build signature maps** for all baseline kernels
2. **Iterate through new kernels** in execution order
3. **For each new kernel**, try to find a match:
   - First: exact name match (identical kernel names)
   - Second: signature match (same base kernel, different config)
4. **Mark unmatched baseline kernels** as "removed"

### Algorithm (from `compare.go`)

```go
// matchBySignature uses greedy signature matching
// Best for compiled vs compiled where kernels may move positions
func matchBySignature(eagerResult, compiledResult *CycleResult) []KernelMatch {
    // 1. Build lookup maps
    eagerBySig := make(map[string][]eagerEntry)   // signature -> kernels
    eagerByName := make(map[string][]eagerEntry)  // exact name -> kernels

    // 2. For each compiled kernel, find best match
    for _, ck := range compiled {
        // Try exact name match first
        if entries, exists := eagerByName[ck.Name]; exists {
            // Use first unmatched entry
        }
        // Then try signature match
        if matched == nil {
            if entries, exists := eagerBySig[sig]; exists {
                // Use first unmatched entry
            }
        }
    }

    // 3. Remaining unmatched eager kernels = "removed"
}
```

### Output Order

1. New kernels in execution order (with matched baseline kernels)
2. Unmatched baseline kernels appended at end as "removed"

### When to Use

- Comparing two compiled traces
- Comparing traces with different optimizations
- When kernels may have moved positions but are fundamentally the same

---

## Mode 2: Position-Based Alignment (`-mode align`, default)

**Default mode.** Best for most comparisons. Includes **automatic cycle rotation detection**.

### How It Works

1. **Detect rotation** (for same-length cycles):
   - Try all rotations of baseline
   - Find rotation with maximum LCS alignment
2. **Build signature arrays** for both sequences
3. **Compute LCS matrix** using dynamic programming
4. **Backtrack** to produce aligned output showing:
   - Matches (kernels in both sequences)
   - Insertions (kernels only in new)
   - Deletions (kernels only in baseline)

### Automatic Rotation Detection

When comparing cycles of the same length, the algorithm automatically finds the best rotation:

```go
// From matchByAlignment()
// Find best rotation of baseline to maximize LCS
bestRotation := 0
bestLCS := computeLCS(eagerSigs, compiledSigs)

if len(eager) == len(compiled) && len(eager) > 0 {
    for rot := 1; rot < len(eager); rot++ {
        rotatedSigs := rotateSlice(eagerSigs, rot)
        lcs := computeLCS(rotatedSigs, compiledSigs)
        if lcs > bestLCS {
            bestLCS = lcs
            bestRotation = rot
        }
    }

    if bestRotation > 0 {
        fmt.Fprintf(os.Stderr, "Detected cycle rotation: baseline rotated by %d positions\n", bestRotation)
        eagerSigs = rotateSlice(eagerSigs, bestRotation)
        eager = rotateKernels(eager, bestRotation)
    }
}
```

**Why rotation matters:** Cycle detection may pick different anchor kernels in different traces, causing the same cycle to start at different positions. Rotation detection ensures optimal alignment.

### LCS Algorithm (from `compare.go`)

```go
// matchByAlignment uses LCS algorithm for position-based alignment
func matchByAlignment(eagerResult, compiledResult *CycleResult) []KernelMatch {
    // Compute LCS matrix
    // lcs[i][j] = length of LCS of eagerSigs[0:i] and compiledSigs[0:j]
    for i := 1; i <= m; i++ {
        for j := 1; j <= n; j++ {
            if eagerSigs[i-1] == compiledSigs[j-1] {
                lcs[i][j] = lcs[i-1][j-1] + 1
            } else {
                lcs[i][j] = max(lcs[i-1][j], lcs[i][j-1])
            }
        }
    }

    // Backtrack to find alignment
    for i > 0 || j > 0 {
        if signatures match {
            // Match (exact or similar)
        } else if prefer deletion from compiled {
            // New kernel (insertion)
        } else {
            // Removed kernel (deletion)
        }
    }
}
```

### Output Order

Interleaved in execution order:
- Matched kernels shown side-by-side
- Removed kernels shown with "." in new column
- New kernels shown with empty baseline column

### When to Use

- Comparing eager mode vs compiled mode
- Comparing different cycles from the same trace (`-mode all`)
- When you want to see exactly where kernels were added/removed
- When execution order is meaningful

---

## Kernel Signature Extraction

Both algorithms use **kernel signatures** to match similar kernels. The signature is a simplified version of the kernel name that groups equivalent kernels together.

### What Gets Removed

From `getKernelSignature()` in `cycle.go`:

| Pattern Type | Examples | Purpose |
|--------------|----------|---------|
| Template parameters | `<float, 128, true>` | Removes type/config instantiation |
| Config suffixes | `_GROUP_K_128`, `_BLOCK_SIZE_256` | Removes eager-mode compile parameters |
| Dimension suffixes | `_32x256`, `_128x64` | Removes block size variations |
| Config prefixes | `_1tg_ps`, `_1tg`, `_ps`, `_vs` | Removes implementation variants |
| Trailing numbers | `_0`, `_1`, `_2` | Removes instance indices |

### Transformation Examples

| Original Kernel Name | Signature |
|---------------------|-----------|
| `gemm_kernel<float, 128, true>` | `gemm_kernel` |
| `aiter::fmoe_bf16_g1u1_vs_silu_1tg_ps_32x256` | `aiter::fmoe_bf16_g1u1` |
| `ck_tile::kentry_GROUP_K_128_BLOCK_SIZE_256` | `ck_tile::kentry` |
| `triton_poi_fused_add_0` | `triton_poi_fused_add` |
| `flash_attn_kernel_32x64` | `flash_attn_kernel` |

### Algorithm (from `cycle.go`)

```go
func getKernelSignature(name string) string {
    sig := name

    // 1. Remove template parameters
    if idx := strings.Index(sig, "<"); idx > 0 {
        sig = sig[:idx]
    }

    // 2. Remove configuration suffixes
    configPatterns := []string{
        "_GROUP_K_", "_GROUP_N_", "_GROUP_SIZE_",
        "_BLOCK_SIZE_", "_SPLITK_BLOCK_SIZE_",
        "_NUM_KSPLIT_", "_ACTUAL_KSPLIT_", "_MAX_KSPLIT_",
        "_GRID_MN_", "_GRID_",
        "_EVEN_K_", "_cache_modifier_",
    }
    for _, pattern := range configPatterns {
        if idx := strings.Index(sig, pattern); idx > 0 {
            sig = sig[:idx]
        }
    }

    // 3. Remove dimension suffixes (NxM pattern)
    // e.g., _32x256, _128x64

    // 4. Remove config suffixes
    configSuffixes := []string{"_1tg_ps", "_1tg", "_ps", "_novs", "_vs"}

    // 5. Remove trailing numbers

    return strings.TrimRight(sig, "_")
}
```

---

## Match Types

| Type | Meaning | XLSX Color |
|------|---------|------------|
| `exact` | Identical kernel names | Light Green |
| `similar` | Same signature, different name (e.g., different template params) | Light Blue |
| `new_only` | Only in new trace | Light Yellow |
| `removed` | Only in baseline trace | Light Red |

---

## Performance Heatmap

The "Change (%)" column uses color coding:

| Change | Color | Meaning |
|--------|-------|---------|
| < -5% | Green | Improvement (faster) |
| -5% to +5% | Orange | Neutral (within noise) |
| > +5% | Red | Regression (slower) |

Special values:
- `NEW` - Kernel only exists in new trace
- `REMOVED` - Kernel only exists in baseline

---

## Choosing the Right Mode

### Use `match` (default) when:
- ✅ Comparing two compiled traces
- ✅ Kernels may have reordered significantly
- ✅ Kernel names might have slight config changes
- ✅ You want maximum matching regardless of position

### Use `align` when:
- ✅ Comparing eager vs compiled
- ✅ Comparing different cycle patterns from same trace
- ✅ Execution order is meaningful
- ✅ You want to see exactly where kernels were fused/split
- ✅ Creating a diff-like view

---

## Order Preservation Metric

To determine if `align` mode will work well, calculate:

```
Order Preservation = LCS Length / Signature Matches × 100%
```

| Preservation | Recommendation |
|--------------|----------------|
| >90% | Use `align` mode |
| 70-90% | Either mode works |
| <70% | Use `match` mode |

Low order preservation indicates significant reordering, where `match` mode will find more pairings.
