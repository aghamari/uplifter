package main

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

// WriteCompareXLSX writes the comparison result to an Excel file
// with heatmap coloring for performance changes
func (r *CompareResult) WriteCompareXLSX(filename string) error {
	f := excelize.NewFile()
	defer f.Close()

	// Create sheet
	sheetName := "Comparison"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return err
	}
	f.SetActiveSheet(index)
	f.DeleteSheet("Sheet1")

	// Define styles
	headerStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#4472C4"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})

	exactStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#E2EFDA"}, Pattern: 1}, // Light green - exact match
	})

	similarStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#DDEBF7"}, Pattern: 1}, // Light blue - similar match
	})

	removedStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFC7CE"}, Pattern: 1}, // Light red - baseline only
	})

	newOnlyStyle, _ := f.NewStyle(&excelize.Style{
		Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFEB9C"}, Pattern: 1}, // Light yellow - new only
	})

	// Heatmap styles for change column
	improvedStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#00B050"}, Pattern: 1}, // Green
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	regressedStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FF0000"}, Pattern: 1}, // Red
		Font:      &excelize.Font{Bold: true, Color: "#FFFFFF"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	neutralStyle, _ := f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FFC000"}, Pattern: 1}, // Orange/amber
		Font:      &excelize.Font{Bold: true},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})

	// Write headers - Baseline vs New naming with Change column
	headers := []string{
		"Baseline Kernel", "Base Avg (µs)", "Base Min", "Base Max", "Base StdDev",
		"New Kernel", "New Avg (µs)", "New Min", "New Max", "New StdDev",
		"Change (%)", "Match Type",
	}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheetName, cell, h)
		f.SetCellStyle(sheetName, cell, cell, headerStyle)
	}

	// Set column widths
	f.SetColWidth(sheetName, "A", "A", 55) // Baseline Kernel
	f.SetColWidth(sheetName, "B", "E", 12) // Baseline stats
	f.SetColWidth(sheetName, "F", "F", 55) // New Kernel
	f.SetColWidth(sheetName, "G", "J", 12) // New stats
	f.SetColWidth(sheetName, "K", "K", 12) // Change (%)
	f.SetColWidth(sheetName, "L", "L", 15) // Match Type

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

		// Column A: Baseline kernel name
		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), baselineStr)

		// Columns B-E: Baseline stats (only if has timing data)
		if m.EagerDur > 0 {
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), m.EagerDur)
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), m.EagerMin)
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), m.EagerMax)
			f.SetCellValue(sheetName, fmt.Sprintf("E%d", row), m.EagerStdDev)
		}

		// Column F: New kernel name
		f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), newStr)

		// Columns G-J: New stats (only if has timing data)
		if m.CompiledKernel != "." && m.CompiledDur > 0 {
			f.SetCellValue(sheetName, fmt.Sprintf("G%d", row), m.CompiledDur)
			f.SetCellValue(sheetName, fmt.Sprintf("H%d", row), m.CompiledMin)
			f.SetCellValue(sheetName, fmt.Sprintf("I%d", row), m.CompiledMax)
			f.SetCellValue(sheetName, fmt.Sprintf("J%d", row), m.CompiledStdDev)
		}

		// Column K: Change (%) with heatmap
		// Negative = improvement (new is faster), Positive = regression (new is slower)
		changeCell := fmt.Sprintf("K%d", row)
		if m.EagerDur > 0 && m.CompiledDur > 0 {
			changePercent := ((m.CompiledDur - m.EagerDur) / m.EagerDur) * 100
			f.SetCellValue(sheetName, changeCell, changePercent)

			// Apply heatmap style based on change
			if changePercent < -5 { // Improved by more than 5%
				f.SetCellStyle(sheetName, changeCell, changeCell, improvedStyle)
			} else if changePercent > 5 { // Regressed by more than 5%
				f.SetCellStyle(sheetName, changeCell, changeCell, regressedStyle)
			} else { // Within ±5% - neutral
				f.SetCellStyle(sheetName, changeCell, changeCell, neutralStyle)
			}
		} else if m.MatchType == "new_only" {
			f.SetCellValue(sheetName, changeCell, "NEW")
			f.SetCellStyle(sheetName, changeCell, changeCell, neutralStyle)
		} else if m.MatchType == "removed" {
			f.SetCellValue(sheetName, changeCell, "REMOVED")
			f.SetCellStyle(sheetName, changeCell, changeCell, improvedStyle)
		}

		// Column L: Match type
		f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), m.MatchType)

		// Apply row style based on match type (excluding change column)
		switch m.MatchType {
		case "exact":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), exactStyle)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), exactStyle)
		case "similar":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), similarStyle)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), similarStyle)
		case "removed":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), removedStyle)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), removedStyle)
		case "new_only":
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("J%d", row), newOnlyStyle)
			f.SetCellStyle(sheetName, fmt.Sprintf("L%d", row), fmt.Sprintf("L%d", row), newOnlyStyle)
		}

		row++

		// Additional rows for multiple baseline kernels
		for i := 1; i < len(m.EagerKernels); i++ {
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), m.EagerKernels[i])
			f.SetCellValue(sheetName, fmt.Sprintf("F%d", row), ".")
			f.SetCellValue(sheetName, fmt.Sprintf("L%d", row), "removed")
			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("L%d", row), removedStyle)
			row++
		}
	}

	// Add auto-filter
	f.AutoFilter(sheetName, fmt.Sprintf("A1:L%d", row-1), nil)

	// Freeze first row
	f.SetPanes(sheetName, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})

	return f.SaveAs(filename)
}

