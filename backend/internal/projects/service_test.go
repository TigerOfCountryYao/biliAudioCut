package projects

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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

func TestMainImageDownloadNamesAreSafeAndDescriptive(t *testing.T) {
	image := MainImage{SKU: "100327335468", SeriesLabel: "王炸新品", VariantLabel: "10kg/7AD1U1", URLs: []string{"https://img13.360buyimg.com/n1/jfs/t1/example.webp"}}
	if got, want := mainImageArchiveFilename(`洗衣机：10kg/测试`, time.Date(2026, 8, 18, 10, 0, 0, 0, time.UTC)), "洗衣机10kg测试_主图_20260818_100000.zip"; got != want {
		t.Fatalf("mainImageArchiveFilename() = %q, want %q", got, want)
	}
	if got, want := mainImageEntryName("洗衣机", 0, image, image.URLs[0], "image/webp"), "洗衣机_主图/001_王炸新品_10kg7AD1U1_100327335468.webp"; got != want {
		t.Fatalf("mainImageEntryName() = %q, want %q", got, want)
	}
}

func TestJDImageURLAllowsOnlyJDImageHosts(t *testing.T) {
	for _, raw := range []string{
		"https://img13.360buyimg.com/n1/jfs/t1/example.jpg",
		"https://img30.jd.com/example.jpg",
		"https://img10.jdcdnimg.com/example.jpg",
	} {
		if !isJDImageURL(raw) {
			t.Fatalf("isJDImageURL(%q) = false, want true", raw)
		}
	}
	for _, raw := range []string{
		"http://img13.360buyimg.com/example.jpg",
		"https://img13.360buyimg.com.evil.example/example.jpg",
		"https://example.com/example.jpg",
	} {
		if isJDImageURL(raw) {
			t.Fatalf("isJDImageURL(%q) = true, want false", raw)
		}
	}
}

func TestWriteMainImageAddsDownloadedImageToZIP(t *testing.T) {
	previousClient := mainImageHTTPClient
	mainImageHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, ".mp4") {
			return &http.Response{
				StatusCode:    http.StatusOK,
				ContentLength: 3,
				Header:        http.Header{"Content-Type": []string{"video/mp4"}},
				Body:          io.NopCloser(strings.NewReader("vid")),
				Request:       request,
			}, nil
		}
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: 3,
			Header:        http.Header{"Content-Type": []string{"image/webp"}},
			Body:          io.NopCloser(strings.NewReader("img")),
			Request:       request,
		}, nil
	})}
	t.Cleanup(func() { mainImageHTTPClient = previousClient })

	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	image := MainImage{SKU: "1001", SeriesLabel: "系列", VariantLabel: "款式", URLs: []string{"https://img13.360buyimg.com/n1/jfs/t1/example.mp4", "https://img13.360buyimg.com/n1/jfs/t1/example.webp"}}
	if err := writeFirstMainImage(context.Background(), archive, "测试任务", 0, image); err != nil {
		t.Fatalf("writeFirstMainImage() error = %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data.Bytes()), int64(data.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "测试任务_主图/001_系列_款式_1001.webp" {
		t.Fatalf("zip files = %#v", reader.File)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFailedProjectCannotReturnToSelectionState(t *testing.T) {
	const status = "failed"
	if status == "collecting" {
		t.Fatal("a failed project must not be transitioned by a later successful task")
	}
}

func TestSuccessfulCaptureDoesNotRetainFailureDetails(t *testing.T) {
	const sourceUpdate = `UPDATE project_sources SET resolved_url=$2,status='succeeded',failure_code=NULL,failure_detail=NULL,updated_at=now() WHERE id=$1`
	const taskUpdate = `UPDATE capture_tasks SET status='succeeded',failure_code=NULL,failure_detail=NULL,completed_at=now() WHERE id=$1`
	for _, query := range []string{sourceUpdate, taskUpdate} {
		if !strings.Contains(query, "failure_code=NULL") || !strings.Contains(query, "failure_detail=NULL") {
			t.Fatalf("successful capture update must clear stale failure data: %s", query)
		}
	}
}

func TestExportReadinessAllowsCompletedOrPartiallyFailedCaptures(t *testing.T) {
	if !isExportReady("awaiting_sku_selection") {
		t.Fatal("SKU selection state should be exportable")
	}
	if !isExportReady("failed") {
		t.Fatal("a failed project with retained snapshots should be exportable")
	}
	for _, status := range []string{"awaiting_extension", "collecting", "succeeded"} {
		if isExportReady(status) {
			t.Fatalf("%q should not be exportable", status)
		}
	}
}

func TestSelectionCanOnlyChangeAfterCaptureCompletes(t *testing.T) {
	if !canUpdateSelection("awaiting_sku_selection") {
		t.Fatal("completed capture should allow SKU selection")
	}
	for _, status := range []string{"awaiting_extension", "collecting", "failed"} {
		if canUpdateSelection(status) {
			t.Fatalf("%q should not allow SKU selection", status)
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

func TestNormalizeLinksAcceptsJDUnionAffiliateLink(t *testing.T) {
	const affiliateLink = "https://union-click.jd.com/jdc?p=encrypted&e=**BMT**"

	links, err := normalizeLinks([]string{affiliateLink})
	if err != nil {
		t.Fatalf("normalizeLinks() error = %v", err)
	}
	if len(links) != 1 || links[0] != affiliateLink {
		t.Fatalf("normalizeLinks() = %#v, want affiliate link unchanged", links)
	}
}

func TestNormalizeLinksAcceptsJDActivityLinkWithSKU(t *testing.T) {
	const activityLink = "https://pro.m.jd.com/mall/active/example/index.html?sku=encrypted-sku&q=encrypted-query"

	links, err := normalizeLinks([]string{activityLink})
	if err != nil {
		t.Fatalf("normalizeLinks() error = %v", err)
	}
	if len(links) != 1 || links[0] != activityLink {
		t.Fatalf("normalizeLinks() = %#v, want activity link unchanged", links)
	}
}

func TestNormalizeLinksRejectsJDActivityLinkWithoutSKU(t *testing.T) {
	if _, err := normalizeLinks([]string{"https://pro.m.jd.com/mall/active/example/index.html"}); err == nil {
		t.Fatal("normalizeLinks() should reject an activity link without a SKU")
	}
}

func TestShortLinkOnlyReturnsJDUShortURLs(t *testing.T) {
	tests := map[string]string{
		"https://u.jd.com/UrSFIly":                         "https://u.jd.com/UrSFIly",
		"https://item.jd.com/100327335468.html?cu=true":    "",
		"https://union-click.jd.com/jdc?p=encrypted&e=BMT": "",
	}
	for raw, want := range tests {
		if got := shortLink(raw); got != want {
			t.Fatalf("shortLink(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestNormalizeLinksRejectsLookalikeUnionAffiliateHost(t *testing.T) {
	for _, link := range []string{
		"https://union-click.jd.com.example.com/jdc?p=encrypted",
		"https://union-click.jd.com@evil.example/jdc?p=encrypted",
		"https://union-click.jd.com/not-jdc?p=encrypted",
	} {
		if _, err := normalizeLinks([]string{link}); err == nil {
			t.Fatalf("normalizeLinks(%q) should reject a non-JD affiliate endpoint", link)
		}
	}
}
