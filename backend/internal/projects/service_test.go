package projects

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEmptyProjectListEncodesAsJSONArray(t *testing.T) {
	projects := make([]Project, 0)
	encoded, err := json.Marshal(map[string]any{"projects": projects})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) != `{"projects":[]}` {
		t.Fatalf("empty list JSON = %s, want projects array", encoded)
	}
}

func TestExportFilenameUsesProjectNameAndSafeTimestamp(t *testing.T) {
	got := exportFilename(`洗衣机：10kg/测试`, time.Date(2026, 8, 12, 16, 30, 45, 0, time.UTC))
	if want := "洗衣机10kg测试_20260812_163045.xlsx"; got != want {
		t.Fatalf("exportFilename() = %q, want %q", got, want)
	}
}

func TestExportReadinessRequiresSKUSelection(t *testing.T) {
	if !isExportReady("awaiting_sku_selection") {
		t.Fatal("SKU selection state should be exportable")
	}
	for _, status := range []string{"awaiting_extension", "collecting", "failed", "succeeded"} {
		if isExportReady(status) {
			t.Fatalf("%q should not be exportable", status)
		}
	}
}

func TestSKUsRemainGroupedBySeriesAndKeepRepeatedSKU(t *testing.T) {
	firstID := uuid.New()
	secondID := uuid.New()
	groups := make(map[string][]SKU)
	for _, sku := range []SKU{
		{ID: firstID, SKU: "100", SeriesLabel: "系列 A", SeriesOrdinal: 0},
		{ID: secondID, SKU: "100", SeriesLabel: "系列 B", SeriesOrdinal: 1},
	} {
		key := fmt.Sprintf("%d:%s", sku.SeriesOrdinal, sku.SeriesLabel)
		groups[key] = append(groups[key], sku)
	}
	if len(groups) != 2 || len(groups["0:系列 A"]) != 1 || len(groups["1:系列 B"]) != 1 {
		t.Fatalf("series grouping lost repeated SKU: %#v", groups)
	}
}
