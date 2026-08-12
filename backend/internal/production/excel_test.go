package production

import (
	"bytes"
	"slices"
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

func TestBuildCaptureWorkbookKeepsSpecificationComparisonColumns(t *testing.T) {
	content, err := BuildCaptureWorkbook(
		[][]string{{"SKU"}, {"100"}, {"200"}},
		[][]string{{"字段来源", "字段名", "款式 A（SKU：100）", "款式 B（SKU：200）"}, {"规格表", "容量", "10kg", "12kg"}},
		[][]string{{"款式名称", "SKU", "图片类型"}, {"款式 A", "100", "款式主图"}},
	)
	if err != nil {
		t.Fatalf("BuildCaptureWorkbook() error = %v", err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer book.Close()
	rows, err := book.GetRows("规格参数")
	if err != nil {
		t.Fatalf("GetRows() error = %v", err)
	}
	if got, want := rows[1], []string{"规格表", "容量", "10kg", "12kg"}; !slices.Equal(got, want) {
		t.Fatalf("comparison row = %#v, want %#v", got, want)
	}
}
