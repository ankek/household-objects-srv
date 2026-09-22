package integration

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLabelsQRBatchRoundTripResolvesOverRealHTTP(t *testing.T) {
	srv := newLiveServer(t, false)

	rec := srv.do(t, http.MethodPost, "/api/v1/auth/register", "", regBody("alice", "alice-correct-horse-battery-qr"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPost, "/api/v1/auth/login", "", regBody("alice", "alice-correct-horse-battery-qr"))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookie(t, rec)

	rec = srv.do(t, http.MethodPost, "/api/v1/items", cookie, itemCreateBody("Cordless Drill"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item: status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var item itemResponse
	mustDecode(t, rec, &item)
	if item.ID == "" || item.ShortCode == "" {
		t.Fatalf("create item: empty id or short_code in %+v", item)
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/labels/qr/batch", cookie, labelsQRBatchBody(item.ID))
	if rec.Code != http.StatusOK {
		t.Fatalf("labels/qr/batch: status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var batch labelsQRBatchResponse
	mustDecode(t, rec, &batch)
	if len(batch.Items) != 1 {
		t.Fatalf("labels/qr/batch: len(Items) = %d, want 1: %s", len(batch.Items), rec.Body.String())
	}
	got := batch.Items[0]
	if got.ID != item.ID {
		t.Fatalf("labels/qr/batch: Items[0].ID = %q, want %q", got.ID, item.ID)
	}
	if got.ShortCode != item.ShortCode {
		t.Fatalf("labels/qr/batch: Items[0].ShortCode = %q, want %q", got.ShortCode, item.ShortCode)
	}

	t.Run("qr_payload_resolves_to_items_page", func(t *testing.T) {
		payload := qrSVGTitlePayload(t, got.QRSVG)
		u, err := url.Parse(payload)
		if err != nil {
			t.Fatalf("parse QR payload %q: %v", payload, err)
		}
		if !strings.HasPrefix(u.Path, "/i/") {
			t.Fatalf("QR payload path = %q, want a /i/ prefix (full payload: %q)", u.Path, payload)
		}
		assertResolvesToItemsPage(t, srv, cookie, u.Path, item.ID)
	})

	t.Run("short_code_resolves_to_items_page", func(t *testing.T) {
		assertResolvesToItemsPage(t, srv, cookie, "/i/"+got.ShortCode, item.ID)
	})
}

func qrSVGTitlePayload(t *testing.T, svg string) string {
	t.Helper()
	const openTag, closeTag = "<title>", "</title>"
	start := strings.Index(svg, openTag)
	if start < 0 {
		t.Fatalf("SVG has no <title> element: %s", svg)
	}
	start += len(openTag)
	end := strings.Index(svg[start:], closeTag)
	if end < 0 {
		t.Fatalf("SVG has no closing </title>: %s", svg)
	}
	return html.UnescapeString(svg[start : start+end])
}

func assertResolvesToItemsPage(t *testing.T, srv *liveServer, cookie, path, wantItemID string) {
	t.Helper()
	rec := srv.do(t, http.MethodGet, path, cookie, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("GET %s: status = %d, want 302: %s", path, rec.Code, rec.Body.String())
	}
	want := "/items/" + wantItemID
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("GET %s: Location = %q, want %q", path, got, want)
	}
}
