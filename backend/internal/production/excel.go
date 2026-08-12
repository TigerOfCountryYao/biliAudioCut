package production

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

func BuildCaptureWorkbook(rows [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "商品对比"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, err := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+1)
			if err != nil {
				return nil, err
			}
			if err := f.SetCellStr(sheet, cell, value); err != nil {
				return nil, err
			}
		}
	}
	if len(rows) > 0 {
		lastColumn, err := excelize.ColumnNumberToName(len(rows[0]))
		if err != nil {
			return nil, err
		}
		if err := f.SetCellStyle(sheet, "A1", fmt.Sprintf("%s1", lastColumn), mustHeaderStyle(f)); err != nil {
			return nil, err
		}
		_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, Split: true, XSplit: 0, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
		for columnIndex := range rows[0] {
			column, _ := excelize.ColumnNumberToName(columnIndex + 1)
			width := 20.0
			if columnIndex == 5 {
				width = 50
			}
			_ = f.SetColWidth(sheet, column, column, width)
		}
	}
	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func mustHeaderStyle(f *excelize.File) int {
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FFFFFF"}, Fill: excelize.Fill{Type: "pattern", Color: []string{"1F4E78"}, Pattern: 1}})
	if err != nil {
		return 0
	}
	return style
}
