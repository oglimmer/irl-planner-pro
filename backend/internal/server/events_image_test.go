package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// withParams layers chi URL params onto a context so handlers that read
// chi.URLParam work when called directly (no router in the test path).
func withParams(ctx context.Context, kv ...string) context.Context {
	rctx := chi.NewRouteContext()
	for i := 0; i+1 < len(kv); i += 2 {
		rctx.URLParams.Add(kv[i], kv[i+1])
	}
	return context.WithValue(ctx, chi.RouteCtxKey, rctx)
}

// tinyPNG is the PNG magic header (+ padding) — enough for http.DetectContentType
// to sniff "image/png". The bytes need not be a decodable image.
var tinyPNG = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 32)...)

func multipartImage(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

// createTestEvent creates an event via the handler and returns its id + slug.
func createTestEvent(t *testing.T, a *App, adminID, slug string) (string, string) {
	t.Helper()
	body, _ := json.Marshal(eventReq{
		Slug: slug, Name: slug, Timezone: "Europe/Paris",
		StartDate: "2099-10-12", EndDate: "2099-10-16",
		SubmissionDeadlineLocal: "2099-10-01T17:00", ReminderHour: 9,
	})
	r := httptest.NewRequest(http.MethodPost, "/api/admin/events", bytes.NewReader(body))
	r = r.WithContext(withAdmin(context.Background(), adminID))
	w := httptest.NewRecorder()
	a.handleCreateEvent(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create event: status %d body %s", w.Code, w.Body.String())
	}
	var e Event
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	return e.ID, e.Slug
}

func TestEventImageUploadServeDelete(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	id, slug := createTestEvent(t, a, admin.ID, "img-offsite")

	// Upload.
	buf, contentType := multipartImage(t, "image", "cover.png", tinyPNG)
	r := httptest.NewRequest(http.MethodPost, "/api/admin/events/"+id+"/image", buf)
	r.Header.Set("Content-Type", contentType)
	r = r.WithContext(withParams(withAdmin(ctx, admin.ID), "id", id))
	w := httptest.NewRecorder()
	a.handleUploadEventImage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("upload: status %d body %s", w.Code, w.Body.String())
	}
	var up struct {
		ImageURL string `json:"imageUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &up); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if !strings.HasPrefix(up.ImageURL, "/api/events/"+slug+"/image?v=") {
		t.Fatalf("unexpected imageUrl: %q", up.ImageURL)
	}

	// The event read now carries the same imageUrl.
	rg := httptest.NewRequest(http.MethodGet, "/api/admin/events/"+id, nil)
	rg = rg.WithContext(withParams(withAdmin(ctx, admin.ID), "id", id))
	wg := httptest.NewRecorder()
	a.handleGetEvent(wg, rg)
	var e Event
	if err := json.Unmarshal(wg.Body.Bytes(), &e); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if e.ImageURL != up.ImageURL {
		t.Errorf("event imageUrl %q != upload %q", e.ImageURL, up.ImageURL)
	}

	// Serve by slug returns the bytes, content type, and an ETag.
	rs := httptest.NewRequest(http.MethodGet, "/api/events/"+slug+"/image", nil)
	rs = rs.WithContext(withParams(ctx, "slug", slug))
	ws := httptest.NewRecorder()
	a.handleGetEventImage(ws, rs)
	if ws.Code != http.StatusOK {
		t.Fatalf("serve: status %d", ws.Code)
	}
	if ct := ws.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("content type %q, want image/png", ct)
	}
	if !bytes.Equal(ws.Body.Bytes(), tinyPNG) {
		t.Error("served bytes differ from uploaded bytes")
	}
	etag := ws.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}

	// A matching If-None-Match yields 304 with no body.
	rc := httptest.NewRequest(http.MethodGet, "/api/events/"+slug+"/image", nil)
	rc.Header.Set("If-None-Match", etag)
	rc = rc.WithContext(withParams(ctx, "slug", slug))
	wc := httptest.NewRecorder()
	a.handleGetEventImage(wc, rc)
	if wc.Code != http.StatusNotModified {
		t.Errorf("conditional GET: status %d, want 304", wc.Code)
	}
	if wc.Body.Len() != 0 {
		t.Error("304 should have an empty body")
	}

	// Delete, then the image is gone and the event read drops imageUrl.
	rd := httptest.NewRequest(http.MethodDelete, "/api/admin/events/"+id+"/image", nil)
	rd = rd.WithContext(withParams(withAdmin(ctx, admin.ID), "id", id))
	wd := httptest.NewRecorder()
	a.handleDeleteEventImage(wd, rd)
	if wd.Code != http.StatusNoContent {
		t.Fatalf("delete: status %d", wd.Code)
	}

	rs2 := httptest.NewRequest(http.MethodGet, "/api/events/"+slug+"/image", nil)
	rs2 = rs2.WithContext(withParams(ctx, "slug", slug))
	ws2 := httptest.NewRecorder()
	a.handleGetEventImage(ws2, rs2)
	if ws2.Code != http.StatusNotFound {
		t.Errorf("serve after delete: status %d, want 404", ws2.Code)
	}
}

func TestEventImageRejectsNonImage(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")
	id, _ := createTestEvent(t, a, admin.ID, "bad-img-offsite")

	buf, contentType := multipartImage(t, "image", "notes.txt", []byte("just some plain text, definitely not an image"))
	r := httptest.NewRequest(http.MethodPost, "/api/admin/events/"+id+"/image", buf)
	r.Header.Set("Content-Type", contentType)
	r = r.WithContext(withParams(withAdmin(ctx, admin.ID), "id", id))
	w := httptest.NewRecorder()
	a.handleUploadEventImage(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-image upload: status %d body %s, want 400", w.Code, w.Body.String())
	}
}
