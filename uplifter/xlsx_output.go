package main

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// xlsxStyles holds all the styles used in XLSX output
type xlsxStyles struct {
	header    int
	exact     int
	similar   int
	removed   int
	newOnly   int
	improved  int
	regressed int
	neutral   int
}

// createStyles creates all styles for the XLSX file
func createStyles(f *excelize.File) xlsxStyles {
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	exactStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E2EFDA"}, Pattern: 1},
	})

	similarStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#DDEBF7"}, Pattern: 1},
	})

	removedStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFC7CE"}, Pattern: 1},
	})

	newOnlyStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFEB9C"}, Pattern: 1},
	})

	improvedStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#00B050"}, Pattern: 1},
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	regressedStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FF0000"}, Pattern: 1},
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	neutralStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FFC000"}, Pattern: 1},
		Font:      &excelize.Font{Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	return xlsxStyles{
		header:    headerStyle,
		exact:     exactStyle,
		similar:   similarStyle,
		removed:   removedStyle,
		newOnly:   newOnlyStyle,
		improved:  improvedStyle,
		regressed: regressedStyle,
		neutral:   neutralStyle,
	}
}

// writeComparisonToSheet writes a comparison result to a specific sheet
func writeComparisonToSheet(f *excelize.File, sheetName string, r *CompareResult, styles xlsxStyles) error {
	// Write headers
	headers := []string{
		"Baseline Kernel", "Base Avg (µs)", "Base Min", "Base Max", "Base StdDev",
		"New Kernel", "New Avg (µs)", "New Min", "New Max", "New StdDev",
		"Change (%)", "Match Type",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
		f.SetCellStyle(sheetName, cell, cell, styles.header)
	}

	// Set column widths
	f.SetColWidth(sheetName, "A", "A", 55)
	f.SetColWidth(sheetName, "B", "E", 12)
	f.SetColWidth(sheetName, "F", "F", 55)
	f.SetColWidth(sheetName, "G", "J", 12)
	f.SetColWidth(sheetName, "K", "K", 12)
	f.SetColWidth(sheetName, "L", "L", 15)

	// Write summary row
	f.SetCellValue(sheetName, "A2", fmt.Sprintf("Total (%d baseline kernels)", r.EagerCycle))
	f.SetCellValue(sheetName, "F2", fmt.Sprintf("(%d new kernels)", r.CompiledCycle))
	f.SetCellValue(sheetName, "G2", r.TotalTime)

	// Write data rows
	row := 3
	for _, m := range r.Matches {
		baselineStr := "(none)"
		if len(m.EagerKernels) > 0 && m.EagerKernels[0] != "(none)" {
			baselineStr = m.EagerKernels[0]
		}

		newStr := m.CompiledKernel

		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), baselineStr)

		if m.EagerDur > 0 {
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), m.EagerDur)
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), m.EagerMin)
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), m.EagerMax)
			f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), m.EagerStdDev)
		}

		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), newStr)

		if m.CompiledKernel != "." && m.CompiledDur > 0 {
			f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), m.CompiledDur)
			f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), m.CompiledMin)
			f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), m.CompiledMax)
			f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), m.CompiledStdDev)
		}

		// Column K: Change (%)
		changeCell := fmt.Sprintf("K%d", row)
		if m.EagerDur > 0 && m.CompiledDur > 0 {
			changePercent := ((m.CompiledDur - m.EagerDur) / m.EagerDur) * 100
			f.SetCellValue(sheetName, changeCell, changePercent)

			if changePercent < -5 {
				f.SetCellStyle(sheetName, changeCell, changeCell, styles.improved)
			} else if changePercent > 5 {
				f.SetCellStyle(sheetName, changeCell, changeCell, styles.regressed)
			} else {
				f.SetCellStyle(sheetName, changeCell, changeCell, styles.neutral)
			}
		} else if m.MatchType == "new_only" {
			f.SetCellValue(sheetName, changeCell, "NEW")
			f.SetCellStyle(sheetName, changeCell, changeCell, styles.neutral)
		} else if m.MatchType == "removed" {
			f.SetCellValue(sheetName, changeCell, "REMOVED")
			f.SetCellStyle(sheetName, changeCell, changeCell, styles.improved)
		}

		f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), m.MatchType)

		// Apply row style
		switch m.MatchType {
		case "exact":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), styles.exact)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), styles.exact)
		case "similar":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), styles.similar)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), styles.similar)
		case "removed":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), styles.removed)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), styles.removed)
		case "new_only":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), styles.newOnly)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), styles.newOnly)
		}

		row++

		for i := 1; i < len(m.EagerKernels); i++ {
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), m.EagerKernels[i])
			f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), ".")
			f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), "removed")
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("L%d", row), styles.removed)
			row++
		}
	}

	// Add auto-filter and freeze
	f.AutoFilter(sheetName, fmt.Sprintf("A1:L%d", row-1), nil)
	f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	return nil
}

// WriteCompareXLSX writes the comparison result to an Excel file
func (r *CompareResult) WriteCompareXLSX(filename string) error {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "Comparison"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")

	styles := createStyles(f)
	if err := writeComparisonToSheet(f, sheetName, r, styles); err != nil {
		return err
	}

	return f.SaveAs(filename)
}

// WriteMultiCompareXLSX writes multiple comparison results to a single Excel file
// Each comparison is written to a separate sheet
func WriteMultiCompareXLSX(filename string, comparisons []*CompareResult, sheetNames []string) error {
	if len(comparisons) == 0 {
		return fmt.Errorf("no comparisons to write")
	}
	if len(sheetNames) != len(comparisons) {
		return fmt.Errorf("number of sheet names must match number of comparisons")
	}

	f := excelize.NewFile()
	defer f.Close()

	styles := createStyles(f)

	for i, result := range comparisons {
		sheetName := sheetNames[i]
		if i == 0 {
			// Rename the default sheet
			f.SetSheetName("Sheet1", sheetName)
		} else {
			_, err := f.NewSheet(sheetName)
			if err != nil {
				return fmt.Errorf("failed to create sheet %s: %v", sheetName, err)
			}
		}

		if err := writeComparisonToSheet(f, sheetName, result, styles); err != nil {
			return fmt.Errorf("failed to write sheet %s: %v", sheetName, err)
		}
	}

	// Set first sheet as active
	if idx, err := f.GetSheetIndex(sheetNames[0]); err == nil {
		f.SetActiveSheet(idx)
	}

	return f.SaveAs(filename)
}
