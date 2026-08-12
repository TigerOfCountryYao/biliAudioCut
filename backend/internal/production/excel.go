package production

import (
	"fmt"
	"github.com/xuri/excelize/v2"
)

func BuildCaptureWorkbook(skus, specs, images [][]string) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()
	sheets := []struct {
		name string
		rows [][]string
	}{{"SKU清单", skus}, {"规格参数", specs}, {"图片清单", images}}
	for index, s := range sheets {
		sheet := s.name
		if index == 0 {
			if err := f.SetSheetName("Sheet1", sheet); err != nil {
				return nil, err
			}
		} else {
			f.NewSheet(sheet)
		}
		for r, row := range s.rows {
			for c, value := range row {
				cell, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					return nil, err
				}
				if err := f.SetCellStr(sheet, cell, value); err != nil {
					return nil, err
				}
			}
		}
		if len(s.rows) > 0 {
			_ = f.SetCellStyle(sheet, "A1", fmt.Sprintf("%s1", string(rune('A'+len(s.rows[0])-1))), mustHeaderStyle(f))
			_ = f.SetPanes(sheet, &excelize.Panes{Freeze: true, Split: true, XSplit: 0, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
			for c := range s.rows[0] {
				cell, _ := excelize.CoordinatesToCellName(c+1, 1)
				_ = f.SetColWidth(sheet, cell, cell, 24)
			}
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
