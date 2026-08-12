package production

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestBuildCaptureWorkbookCreatesThreeSheets(t *testing.T) {
	content, err := BuildCaptureWorkbook(
		[][]string{{"SKU"}, {"100"}},
		[][]string{{"字段名"}, {"型号"}},
		[][]string{{"图片类型"}, {"main"}},
	)
	if err != nil {
		t.Fatalf("BuildCaptureWorkbook() error = %v", err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer book.Close()
	for _, sheet := range []string{"SKU清单", "规格参数", "图片清单"} {
		index, err := book.GetSheetIndex(sheet)
		if err != nil || index == -1 {
			t.Errorf("sheet %q does not exist", sheet)
		}
	}
}
