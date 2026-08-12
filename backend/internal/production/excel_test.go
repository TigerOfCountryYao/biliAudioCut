package production

import (
	"bytes"
	"slices"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestBuildCaptureWorkbookCreatesOneComparisonSheet(t *testing.T) {
	content, err := BuildCaptureWorkbook([][]string{
		{"系列品", "款式名称", "SKU", "规格表：容量"},
		{"系列 A", "款式 A", "100", "10kg"},
		{"系列 A", "款式 B", "200", "12kg"},
	})
	if err != nil {
		t.Fatalf("BuildCaptureWorkbook() error = %v", err)
	}
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer book.Close()
	if got, want := book.GetSheetList(), []string{"商品对比"}; !slices.Equal(got, want) {
		t.Fatalf("sheets = %#v, want %#v", got, want)
	}
	rows, err := book.GetRows("商品对比")
	if err != nil {
		t.Fatalf("GetRows() error = %v", err)
	}
	if got, want := rows[2], []string{"系列 A", "款式 B", "200", "12kg"}; !slices.Equal(got, want) {
		t.Fatalf("comparison row = %#v, want %#v", got, want)
	}
}
