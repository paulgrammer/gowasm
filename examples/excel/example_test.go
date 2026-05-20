package excel_test

import (
	"fmt"

	"example.com/excel"
)

// invoice builds a small workbook with a header, typed numbers and a total
// formula, which is what makes this a spreadsheet rather than a CSV.
func invoice() []byte {
	data, _ := excel.Write([]excel.SheetSpec{{
		Name:   "Invoice",
		Freeze: true,
		Columns: []excel.Column{
			{Header: "Item", Width: 24},
			{Header: "Qty", Width: 8},
			{Header: "Price", Width: 12, Format: "#,##0.00"},
		},
		Rows: [][]string{
			{"Widget", "3", "19.99"},
			{"Gasket", "10", "2.50"},
		},
		Formulas: []excel.Cell{
			{Ref: "B4", Formula: "SUM(B2:B3)"},
			{Ref: "C4", Formula: "SUMPRODUCT(B2:B3,C2:C3)"},
		},
	}})
	return data
}

func ExampleWrite() {
	data := invoice()
	// The magic bytes of a zip archive, which is what an xlsx file is.
	fmt.Println(len(data) > 0, string(data[:2]))
	// Output: true PK
}

func ExampleWrite_noSheets() {
	_, err := excel.Write(nil)
	fmt.Println(err)
	// Output: a workbook needs at least one sheet
}

func ExampleWrite_unnamedSheet() {
	_, err := excel.Write([]excel.SheetSpec{{Name: "  "}})
	fmt.Println(err)
	// Output: sheet 1 has no name
}

func ExampleSheetNames() {
	names, _ := excel.SheetNames(invoice())
	fmt.Println(names)
	// Output: [Invoice]
}

func ExampleSheetNames_notAWorkbook() {
	_, err := excel.SheetNames([]byte("this is not a spreadsheet"))
	fmt.Println(err != nil)
	// Output: true
}

func ExampleRead() {
	wb, _ := excel.Read(invoice())
	s := wb.Sheets[0]
	fmt.Println(s.Name, len(s.Rows))
	fmt.Println(s.Rows[0])
	fmt.Println(s.Rows[1])
	// Output:
	// Invoice 4
	// [Item Qty Price]
	// [Widget 3 19.99]
}

func ExampleGetCell() {
	v, _ := excel.GetCell(invoice(), "Invoice", "A2")
	fmt.Println(v)
	// Output: Widget
}

func ExampleRead_preservesFormulas() {
	wb, _ := excel.Read(invoice())
	for _, c := range wb.Sheets[0].Cells {
		if c.Formula != "" {
			fmt.Println(c.Ref, c.Formula)
		}
	}
	// Output:
	// B4 SUM(B2:B3)
	// C4 SUMPRODUCT(B2:B3,C2:C3)
}
