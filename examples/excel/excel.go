// Package excel reads and writes real .xlsx workbooks.
//
// This is the example with the clearest practical draw. The JavaScript
// ecosystem's answer is SheetJS, whose maintained edition is commercial; the
// free fork is old. Everything else handles a subset of the format.
//
// excelize writes actual Office Open XML: multiple sheets, formulas that Excel
// recalculates, cell styles, column widths, merged cells. It works entirely in
// memory here, so a spreadsheet can be built or parsed in a browser tab and
// never touch a server. For anything involving payroll, invoices or patient
// data, that is not a performance detail but the whole point.
package excel

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Cell is one populated cell.
type Cell struct {
	// Ref is the A1-style reference, such as "B7".
	Ref   string `json:"ref"`
	Value string `json:"value"`
	// Formula is set when the cell holds one, without its leading equals sign.
	Formula string `json:"formula,omitempty"`
}

// Sheet is one worksheet's contents.
type Sheet struct {
	Name string `json:"name"`
	// Rows holds every row as a slice of cell values, left to right.
	Rows [][]string `json:"rows"`
	// Cells lists only the populated cells, with their references.
	Cells []Cell `json:"cells"`
}

// Workbook describes a parsed file.
type Workbook struct {
	Sheets []Sheet `json:"sheets"`
	// SheetNames is the tab order.
	SheetNames []string `json:"sheetNames"`
}

// Column describes one column of a generated sheet.
type Column struct {
	Header string `json:"header"`
	// Width is the column width in characters. Zero means leave it alone.
	Width float64 `json:"width,omitempty"`
	// Format is a number format such as "#,##0.00" or "yyyy-mm-dd".
	Format string `json:"format,omitempty"`
}

// SheetSpec describes a sheet to create.
type SheetSpec struct {
	Name    string     `json:"name"`
	Columns []Column   `json:"columns"`
	Rows    [][]string `json:"rows"`
	// Formulas are written after the rows, so a total row can reference them.
	Formulas []Cell `json:"formulas,omitempty"`
	// Freeze keeps the header row visible while scrolling.
	Freeze bool `json:"freeze,omitempty"`
}

// Read parses a workbook and returns every sheet.
func Read(data []byte) (Workbook, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return Workbook{}, fmt.Errorf("not a readable workbook: %w", err)
	}
	defer f.Close()

	wb := Workbook{Sheets: []Sheet{}, SheetNames: f.GetSheetList()}
	for _, name := range wb.SheetNames {
		rows, err := f.GetRows(name)
		if err != nil {
			return Workbook{}, fmt.Errorf("reading sheet %q: %w", name, err)
		}
		if rows == nil {
			rows = [][]string{}
		}

		// Width is taken from the widest row, because a formula cell carries no
		// cached value and so does not extend its own row in GetRows.
		width := 0
		for _, row := range rows {
			width = max(width, len(row))
		}

		cells := []Cell{}
		for r := range rows {
			for c := range width {
				ref, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					continue
				}
				value := ""
				if c < len(rows[r]) {
					value = rows[r][c]
				}
				formula, _ := f.GetCellFormula(name, ref)
				// A cell with a formula but no cached value is still a cell; it
				// is what an unopened workbook looks like before Excel
				// recalculates it.
				if strings.TrimSpace(value) == "" && formula == "" {
					continue
				}
				cells = append(cells, Cell{Ref: ref, Value: value, Formula: formula})
			}
		}
		wb.Sheets = append(wb.Sheets, Sheet{Name: name, Rows: rows, Cells: cells})
	}
	return wb, nil
}

// SheetNames lists the sheets in a workbook without reading their contents.
func SheetNames(data []byte) ([]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("not a readable workbook: %w", err)
	}
	defer f.Close()
	return f.GetSheetList(), nil
}

// Cell reads a single cell's value.
func GetCell(data []byte, sheet, ref string) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("not a readable workbook: %w", err)
	}
	defer f.Close()

	v, err := f.GetCellValue(sheet, ref)
	if err != nil {
		return "", fmt.Errorf("reading %s!%s: %w", sheet, ref, err)
	}
	return v, nil
}

// Write builds a workbook from the given sheets and returns the file.
//
// The result is a real .xlsx: Excel, Numbers and LibreOffice all open it, and
// the formulas recalculate rather than being frozen values.
func Write(sheets []SheetSpec) ([]byte, error) {
	if len(sheets) == 0 {
		return nil, fmt.Errorf("a workbook needs at least one sheet")
	}

	f := excelize.NewFile()
	defer f.Close()

	// A new file always contains Sheet1; it is removed at the end unless one of
	// the requested sheets happens to be called that.
	const initial = "Sheet1"
	keepInitial := false

	for i, spec := range sheets {
		if strings.TrimSpace(spec.Name) == "" {
			return nil, fmt.Errorf("sheet %d has no name", i+1)
		}
		if spec.Name == initial {
			keepInitial = true
		} else if _, err := f.NewSheet(spec.Name); err != nil {
			return nil, fmt.Errorf("creating sheet %q: %w", spec.Name, err)
		}

		if err := writeSheet(f, spec); err != nil {
			return nil, err
		}
	}

	if !keepInitial {
		if idx, err := f.GetSheetIndex(initial); err == nil && idx >= 0 {
			if err := f.DeleteSheet(initial); err != nil {
				return nil, fmt.Errorf("removing the default sheet: %w", err)
			}
		}
	}
	if idx, err := f.GetSheetIndex(sheets[0].Name); err == nil {
		f.SetActiveSheet(idx)
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("writing the workbook: %w", err)
	}
	return buf.Bytes(), nil
}

func writeSheet(f *excelize.File, spec SheetSpec) error {
	row := 1

	if len(spec.Columns) > 0 {
		bold, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
		if err != nil {
			return fmt.Errorf("creating the header style: %w", err)
		}
		for i, col := range spec.Columns {
			ref, err := excelize.CoordinatesToCellName(i+1, row)
			if err != nil {
				return err
			}
			if err := f.SetCellStr(spec.Name, ref, col.Header); err != nil {
				return fmt.Errorf("writing header %q: %w", col.Header, err)
			}
			if err := f.SetCellStyle(spec.Name, ref, ref, bold); err != nil {
				return err
			}

			letter, err := excelize.ColumnNumberToName(i + 1)
			if err != nil {
				return err
			}
			if col.Width > 0 {
				if err := f.SetColWidth(spec.Name, letter, letter, col.Width); err != nil {
					return err
				}
			}
			if col.Format != "" {
				style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &col.Format})
				if err != nil {
					return fmt.Errorf("creating the format for %q: %w", col.Header, err)
				}
				if err := f.SetColStyle(spec.Name, letter, style); err != nil {
					return err
				}
			}
		}
		row++
	}

	for _, values := range spec.Rows {
		for i, v := range values {
			ref, err := excelize.CoordinatesToCellName(i+1, row)
			if err != nil {
				return err
			}
			// Written through SetCellValue so numbers land as numbers and text
			// as text, which is what makes the formulas below work.
			if err := f.SetCellValue(spec.Name, ref, typed(v)); err != nil {
				return fmt.Errorf("writing %s: %w", ref, err)
			}
		}
		row++
	}

	for _, c := range spec.Formulas {
		if err := f.SetCellFormula(spec.Name, c.Ref, c.Formula); err != nil {
			return fmt.Errorf("writing the formula in %s: %w", c.Ref, err)
		}
	}

	if spec.Freeze && len(spec.Columns) > 0 {
		if err := f.SetPanes(spec.Name, &excelize.Panes{
			Freeze: true, Split: false, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
		}); err != nil {
			return fmt.Errorf("freezing the header: %w", err)
		}
	}
	return nil
}

// typed converts a string to a number when it looks like one, so that a column
// of figures is numeric in the file rather than text that merely looks numeric.
func typed(s string) any {
	t := strings.TrimSpace(s)
	if t == "" {
		return s
	}
	var f float64
	if _, err := fmt.Sscanf(t, "%g", &f); err == nil {
		// Sscanf accepts a numeric prefix, so confirm it consumed everything.
		if formatted := strings.TrimSpace(fmt.Sprintf("%v", f)); formatted == t || fmt.Sprintf("%.0f", f) == t {
			return f
		}
	}
	return s
}
