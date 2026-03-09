package media

import (
	"encoding/base64"
	"fmt"
	"testing"
)

func TestOcrImage_NoTesseract(t *testing.T) {
	ext := &Extractor{tesseractPath: ""} // no tesseract
	result, err := ext.ocrImage([]byte("fake image data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "tesseract not installed" {
		t.Errorf("expected 'tesseract not installed', got %s", result.Error)
	}
	if result.FileType != TypeImage {
		t.Errorf("expected TypeImage, got %s", result.FileType)
	}
}

func TestExtractPDF_NoPdftotext(t *testing.T) {
	ext := &Extractor{pdfToTextPath: ""} // no pdftotext
	result, err := ext.extractPDF([]byte("fake pdf data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error != "pdftotext not installed" {
		t.Errorf("expected 'pdftotext not installed', got %s", result.Error)
	}
	if result.FileType != TypePDF {
		t.Errorf("expected TypePDF, got %s", result.FileType)
	}
}

func TestOcrImage_InvalidTesseractPath(t *testing.T) {
	ext := &Extractor{tesseractPath: "/nonexistent/tesseract"}
	result, err := ext.ocrImage([]byte("fake image"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return error in result (not Go error)
	if result.Error == "" {
		t.Error("should report tesseract execution error")
	}
}

func TestExtractPDF_InvalidPdftotextPath(t *testing.T) {
	ext := &Extractor{pdfToTextPath: "/nonexistent/pdftotext"}
	result, err := ext.extractPDF([]byte("fake pdf"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == "" {
		t.Error("should report pdftotext execution error")
	}
}

func TestScanOpenAIMessages_BadJSON(t *testing.T) {
	ext := New()
	results := ext.ScanOpenAIMessages([]byte("not json"))
	if results != nil {
		t.Error("invalid JSON should return nil")
	}
}

func TestScanOpenAIMessages_StringContent(t *testing.T) {
	ext := New()
	body := `{"messages":[{"role":"user","content":"just text, no images"}]}`
	results := ext.ScanOpenAIMessages([]byte(body))
	if len(results) != 0 {
		t.Errorf("string content should return 0 results, got %d", len(results))
	}
}

func TestScanOpenAIMessages_NonImageBlock(t *testing.T) {
	ext := New()
	body := `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`
	results := ext.ScanOpenAIMessages([]byte(body))
	if len(results) != 0 {
		t.Errorf("text block should return 0 results, got %d", len(results))
	}
}

func TestScanOpenAIMessages_ImageBlockNoDataURI(t *testing.T) {
	ext := New()
	body := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]}]}`
	results := ext.ScanOpenAIMessages([]byte(body))
	// URL (not data URI) should be skipped
	if len(results) != 0 {
		t.Errorf("non-data URI should return 0 results, got %d", len(results))
	}
}

func TestScanOpenAIMessages_ImageBlockWithBase64(t *testing.T) {
	ext := New()
	// Create a tiny valid PNG-like base64
	fakeImg := base64.StdEncoding.EncodeToString([]byte("fake png data for testing"))
	body := fmt.Sprintf(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,%s"}}]}]}`, fakeImg)
	results := ext.ScanOpenAIMessages([]byte(body))
	// Should attempt extraction (result may have error since tesseract not installed)
	if len(results) != 1 {
		t.Errorf("should return 1 result, got %d", len(results))
	}
}

func TestScanOpenAIMessages_EmptyMessages(t *testing.T) {
	ext := New()
	body := `{"messages":[]}`
	results := ext.ScanOpenAIMessages([]byte(body))
	if len(results) != 0 {
		t.Errorf("empty messages should return 0 results, got %d", len(results))
	}
}

func TestScanOpenAIMessages_NullImageURL(t *testing.T) {
	ext := New()
	body := `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":null}]}]}`
	results := ext.ScanOpenAIMessages([]byte(body))
	if len(results) != 0 {
		t.Errorf("null image_url should return 0 results, got %d", len(results))
	}
}

func TestDetectFileType_Unknown(t *testing.T) {
	ft := DetectFileType("application/zip")
	if ft != "" {
		t.Errorf("unknown MIME should return empty, got %s", ft)
	}
}

func TestDetectFileType_PDF(t *testing.T) {
	ft := DetectFileType("application/pdf")
	if ft != TypePDF {
		t.Errorf("expected TypePDF, got %s", ft)
	}
}

func TestDetectFileType_Image(t *testing.T) {
	for _, mime := range []string{"image/png", "image/jpeg", "image/gif", "image/webp"} {
		ft := DetectFileType(mime)
		if ft != TypeImage {
			t.Errorf("MIME %s should return TypeImage, got %s", mime, ft)
		}
	}
}

func TestExtractBase64FromDataURI_Valid(t *testing.T) {
	data, mime, ok := ExtractBase64FromDataURI("data:image/png;base64,aGVsbG8=")
	if !ok {
		t.Error("should return ok for valid data URI")
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}
	if data != "aGVsbG8=" {
		t.Errorf("unexpected data: %s", data)
	}
}

func TestExtractBase64FromDataURI_NotDataURI(t *testing.T) {
	_, _, ok := ExtractBase64FromDataURI("https://example.com/image.png")
	if ok {
		t.Error("URL should not be a valid data URI")
	}
}

func TestExtractFromBytes_UnknownType(t *testing.T) {
	ext := New()
	_, err := ext.ExtractFromBytes([]byte("data"), "unknown_type")
	if err == nil {
		t.Error("unknown file type should return error")
	}
}
