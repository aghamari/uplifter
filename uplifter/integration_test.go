package main

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Integration tests to verify cycle detection and comparison functionality
// Run with: go test -v -run Integration

// TestIntegrationCompareCsvDecode tests the compare-csv functionality for decode phase
func TestIntegrationCompareCsvDecode(t *testing.T) {
	// Read expected CSVs
	eagerKernels, err := readKernelStatsFromCSV("testdata/expected_eager_conc32_decode.csv")
	if err != nil {
		t.Fatalf("Failed to read eager CSV: %v", err)
	}

	compiledKernels, err := readKernelStatsFromCSV("testdata/expected_conc32_decode.csv")
	if err != nil {
		t.Fatalf("Failed to read compiled CSV: %v", err)
	}

	// Verify expected kernel counts
	if len(eagerKernels) != 31 {
		t.Errorf("Expected 31 eager kernels, got %d", len(eagerKernels))
	}
	if len(compiledKernels) != 28 {
		t.Errorf("Expected 28 compiled kernels, got %d", len(compiledKernels))
	}

	// Create mock CycleResults
	eagerResult := &CycleResult{
		CycleLength: len(eagerKernels),
		Kernels:     eagerKernels,
	}
	compiledResult := &CycleResult{
		CycleLength: len(compiledKernels),
		Kernels:     compiledKernels,
	}

	// Run matching
	matches := matchKernelsBySignature(eagerResult, compiledResult)

	// Verify match counts
	matchCounts := countMatchTypes(matches)

	// Based on current output: exact: 9, similar: 8, fused: 14, compiled_only: 11
	if matchCounts["exact"] < 8 {
		t.Errorf("Expected at least 8 exact matches, got %d", matchCounts["exact"])
	}
	if matchCounts["fused"] < 10 {
		t.Errorf("Expected at least 10 fused kernels, got %d", matchCounts["fused"])
	}

	// Verify fmoe kernel is matched exactly
	fmoeMatched := false
	for _, m := range matches {
		if m.MatchType == "exact" && 
			containsSubstring(m.CompiledKernel, "fmoe_bf16_blockscaleFp8") {
			fmoeMatched = true
			break
		}
	}
	if !fmoeMatched {
		t.Error("Expected fmoe kernel to have exact match")
	}
}

// TestIntegrationCompareBaselineVsNew tests comparing baseline vs new (both with timing)
func TestIntegrationCompareBaselineVsNew(t *testing.T) {
	baselineKernels, err := readKernelStatsFromCSV("testdata/expected_baseline_decode.csv")
	if err != nil {
		t.Fatalf("Failed to read baseline CSV: %v", err)
	}

	newKernels, err := readKernelStatsFromCSV("testdata/expected_fmoe_decode.csv")
	if err != nil {
		t.Fatalf("Failed to read new CSV: %v", err)
	}

	// Both should have 17 kernels for decode
	if len(baselineKernels) != 17 {
		t.Errorf("Expected 17 baseline kernels, got %d", len(baselineKernels))
	}
	if len(newKernels) != 17 {
		t.Errorf("Expected 17 new kernels, got %d", len(newKernels))
	}

	baselineResult := &CycleResult{
		CycleLength: len(baselineKernels),
		Kernels:     baselineKernels,
	}
	newResult := &CycleResult{
		CycleLength: len(newKernels),
		Kernels:     newKernels,
	}

	matches := matchKernelsBySignature(baselineResult, newResult)
	matchCounts := countMatchTypes(matches)

	// Most should be exact matches
	if matchCounts["exact"] < 14 {
		t.Errorf("Expected at least 14 exact matches, got %d", matchCounts["exact"])
	}

	// Verify timing data is preserved
	for _, m := range matches {
		if m.MatchType == "exact" && m.EagerDur > 0 && m.CompiledDur > 0 {
			// Both should have timing
			if m.EagerMin == 0 || m.CompiledMin == 0 {
				t.Error("Expected min duration to be set for matched kernels with timing")
			}
		}
	}
}

// TestIntegrationPrefillCycleCount tests prefill cycle detection
func TestIntegrationPrefillCycleCount(t *testing.T) {
	kernels, err := readKernelStatsFromCSV("testdata/expected_baseline_prefill.csv")
	if err != nil {
		t.Fatalf("Failed to read prefill CSV: %v", err)
	}

	// Prefill should have 25 kernels per cycle
	if len(kernels) != 25 {
		t.Errorf("Expected 25 prefill kernels, got %d", len(kernels))
	}

	// Verify fmoe kernel exists with reasonable timing
	fmoeFound := false
	for _, k := range kernels {
		if containsSubstring(k.Name, "fmoe_bf16_blockscaleFp8") {
			fmoeFound = true
			// Prefill fmoe should be > 400µs typically
			if k.AvgDur < 100 {
				t.Errorf("Prefill fmoe duration seems too low: %.2f µs", k.AvgDur)
			}
			break
		}
	}
	if !fmoeFound {
		t.Error("Expected to find fmoe kernel in prefill cycle")
	}
}

// TestGetKernelSignature tests the signature extraction function
func TestGetKernelSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "kernel with template",
			input:    "void ck::kernel_gemm<int, float>",
			expected: "void ck::kernel_gemm",
		},
		{
			name:     "triton kernel with trailing number",
			input:    "triton_red_fused__to_copy_add_mean_mul_pow_rsqrt_0",
			expected: "triton_red_fused__to_copy_add_mean_mul_pow_rsqrt",
		},
		{
			name:     "simple kernel unchanged",
			input:    "simple_kernel_name",
			expected: "simple_kernel_name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getKernelSignature(tt.input)
			if got != tt.expected {
				t.Errorf("getKernelSignature(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestSignatureMatchesSimilarKernels verifies kernels with same signature match
func TestSignatureMatchesSimilarKernels(t *testing.T) {
	// These should have the same signature
	kernel1 := "void ck::kernel_gemm<int, float, 32>"
	kernel2 := "void ck::kernel_gemm<long, double, 64>"
	
	sig1 := getKernelSignature(kernel1)
	sig2 := getKernelSignature(kernel2)
	
	if sig1 != sig2 {
		t.Errorf("Expected same signature for similar kernels, got %q vs %q", sig1, sig2)
	}
}

// Helper functions

func readKernelStatsFromCSV(filename string) ([]KernelStats, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}

	// Find column indices
	colIdx := make(map[string]int)
	for i, col := range header {
		colIdx[col] = i
	}

	var kernels []KernelStats
	for {
		record, err := reader.Read()
		if err != nil {
			break
		}

		k := KernelStats{}
		if idx, ok := colIdx["kernel_name"]; ok && idx < len(record) {
			k.Name = record[idx]
		}
		if idx, ok := colIdx["avg_duration_us"]; ok && idx < len(record) {
			k.AvgDur, _ = strconv.ParseFloat(record[idx], 64)
		}
		if idx, ok := colIdx["min_duration_us"]; ok && idx < len(record) {
			k.MinDur, _ = strconv.ParseFloat(record[idx], 64)
		}
		if idx, ok := colIdx["max_duration_us"]; ok && idx < len(record) {
			k.MaxDur, _ = strconv.ParseFloat(record[idx], 64)
		}
		if idx, ok := colIdx["stddev_us"]; ok && idx < len(record) {
			k.StdDev, _ = strconv.ParseFloat(record[idx], 64)
		}

		kernels = append(kernels, k)
	}

	return kernels, nil
}

func countMatchTypes(matches []KernelMatch) map[string]int {
	counts := make(map[string]int)
	for _, m := range matches {
		counts[m.MatchType]++
	}
	return counts
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && 
		(s == substr || 
		 len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// floatClose checks if two floats are close (within tolerance)
func floatClose(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// getTestDataPath returns the path to test data files
func getTestDataPath(filename string) string {
	return filepath.Join("testdata", filename)
}

