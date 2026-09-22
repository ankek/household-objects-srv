package qrlabels

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/boombuler/barcode/qr"
	"strings"
	"testing"
)

func TestItemURL(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		itemID string
		want   string
	}{
		{
			name:   "no trailing slash",
			origin: "https://hho.example.com",
			itemID: "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
			want:   "https://hho.example.com/i/0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
		},
		{
			name:   "trailing slash is trimmed",
			origin: "https://hho.example.com/",
			itemID: "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
			want:   "https://hho.example.com/i/0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
		},
		{
			name:   "origin with a mount path keeps the path, trims only trailing slashes",
			origin: "https://hho.example.com/hho///",
			itemID: "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
			want:   "https://hho.example.com/hho/i/0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
		},
		{
			name:   "non-standard port is preserved",
			origin: "http://192.168.1.20:8080",
			itemID: "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
			want:   "http://192.168.1.20:8080/i/0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ItemURL(tt.origin, tt.itemID); got != tt.want {
				t.Errorf("ItemURL(%q, %q) = %q, want %q", tt.origin, tt.itemID, got, tt.want)
			}
		})
	}
}

func TestSVGEmptyPayloadErrors(t *testing.T) {
	_, err := SVG("")
	if !errors.Is(err, ErrEmptyPayload) {
		t.Fatalf("SVG(\"\") error = %v, want ErrEmptyPayload", err)
	}
}

func TestSVGDeterministic(t *testing.T) {
	payload := ItemURL("https://hho.example.com", "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef")

	first, err := SVG(payload)
	if err != nil {
		t.Fatalf("SVG() error = %v", err)
	}
	second, err := SVG(payload)
	if err != nil {
		t.Fatalf("SVG() error = %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Errorf("SVG(%q) is not deterministic:\nfirst:  %s\nsecond: %s", payload, first, second)
	}
}

type svgDoc struct {
	XMLName xml.Name `xml:"svg"`
	ViewBox string   `xml:"viewBox,attr"`
	Title   string   `xml:"title"`
	Rect    struct {
		Width  string `xml:"width,attr"`
		Height string `xml:"height,attr"`
		Fill   string `xml:"fill,attr"`
	} `xml:"rect"`
	Path struct {
		D    string `xml:"d,attr"`
		Fill string `xml:"fill,attr"`
	} `xml:"path"`
}

func TestSVGIsValidXML(t *testing.T) {
	payload := ItemURL("https://hho.example.com", "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef")

	out, err := SVG(payload)
	if err != nil {
		t.Fatalf("SVG() error = %v", err)
	}

	var doc svgDoc
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("SVG() produced invalid XML: %v\noutput: %s", err, out)
	}

	if doc.ViewBox == "" {
		t.Error("svg viewBox attribute is empty, want a \"0 0 N N\" box")
	}
	if doc.Title != payload {
		t.Errorf("svg title = %q, want %q", doc.Title, payload)
	}
	if doc.Rect.Fill != "#ffffff" {
		t.Errorf("rect fill = %q, want #ffffff", doc.Rect.Fill)
	}
	if doc.Path.Fill != "#000000" {
		t.Errorf("path fill = %q, want #000000", doc.Path.Fill)
	}
	if doc.Path.D == "" {
		t.Error("path d attribute is empty, want at least one module drawn")
	}

	s := string(out)
	start := strings.Index(s, "<svg")
	if start < 0 {
		t.Fatalf("SVG output has no <svg> tag: %s", out)
	}
	end := strings.Index(s[start:], ">")
	if end < 0 {
		t.Fatalf("SVG output has an unterminated <svg> tag: %s", out)
	}
	svgTag := s[start : start+end]
	if strings.Contains(svgTag, "width=") || strings.Contains(svgTag, "height=") {
		t.Errorf("root <svg> element carries a width/height attribute (%q); it must scale via viewBox only", svgTag)
	}
}

func TestSVGEscapesPayload(t *testing.T) {
	malicious := `https://hho.example.com/i/"><script>alert(1)</script>&<img/onerror=1>`

	out, err := SVG(malicious)
	if err != nil {
		t.Fatalf("SVG() error = %v", err)
	}

	for _, marker := range []string{"<script>", "<img/onerror", "\"><script"} {
		if strings.Contains(string(out), marker) {
			t.Errorf("SVG output contains unescaped injection marker %q:\n%s", marker, out)
		}
	}

	dec := xml.NewDecoder(bytes.NewReader(out))
	allowed := map[string]bool{"svg": true, "title": true, "rect": true, "path": true}
	count := 0
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok {
			count++
			if !allowed[se.Name.Local] {
				t.Errorf("SVG output contains unexpected element %q (payload injection?)", se.Name.Local)
			}
		}
	}
	if count != 4 {
		t.Errorf("SVG output has %d elements, want exactly 4 (svg, title, rect, path)", count)
	}

	var doc svgDoc
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("SVG() with malicious payload produced invalid XML: %v\noutput: %s", err, out)
	}
	if doc.Title != malicious {
		t.Errorf("svg title = %q, want %q (payload should round-trip through escaping unchanged)", doc.Title, malicious)
	}
}

func TestSVGModuleMatrixSizeForLongURL(t *testing.T) {
	origin := "https://home-inventory.io"
	itemID := "0198c1c2-1a2b-7c3d-8e4f-abcdefabcdef"
	payload := ItemURL(origin, itemID)
	if len(payload) != 64 {
		t.Fatalf("test fixture payload is %d bytes, want exactly 64 to match this test's name/intent", len(payload))
	}

	wantCode, err := qr.Encode(payload, errorCorrectionLevel, qr.Unicode)
	if err != nil {
		t.Fatalf("qr.Encode() error = %v", err)
	}
	wantDim := wantCode.Bounds().Dx()
	wantSize := wantDim + 2*quietZoneModules

	out, err := SVG(payload)
	if err != nil {
		t.Fatalf("SVG() error = %v", err)
	}
	var doc svgDoc
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("SVG() produced invalid XML: %v", err)
	}

	wantViewBox := fmt.Sprintf("0 0 %d %d", wantSize, wantSize)
	if doc.ViewBox != wantViewBox {
		t.Errorf("viewBox = %q, want %q (encoder module dimension %d + 2*%d quiet zone)", doc.ViewBox, wantViewBox, wantDim, quietZoneModules)
	}
}
