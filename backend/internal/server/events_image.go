package server

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"irlplanner/internal/metrics"
)

// eventImageCap bounds the uploaded image size. Matches the nginx 4m body limit
// (a larger image is rejected at the proxy before reaching here) and is small
// enough that holding one in memory per request is cheap.
const eventImageCap = 4 << 20 // 4 MiB

// allowedImageTypes are the content types accepted for an event cover image,
// matched against the server-sniffed type (never the client-claimed one).
var allowedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// handleUploadEventImage stores (or replaces) an event's cover image. The image
// lives out-of-line in event_images keyed by event_id, so this is a single
// upsert; the content type is sniffed server-side and the etag is the content
// hash that drives HTTP caching and the ?v= cache-buster on the public URL.
func (a *App) handleUploadEventImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Confirm the event exists and grab its slug for the response URL.
	var slug string
	err := a.DB.QueryRowContext(r.Context(), `SELECT slug FROM events WHERE id = $1`, id).Scan(&slug)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, eventImageCap)
	if err := r.ParseMultipartForm(eventImageCap); err != nil {
		writeErr(w, http.StatusBadRequest, "file too large or invalid upload")
		return
	}
	file, _, err := r.FormFile("image")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing image field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "could not read upload")
		return
	}
	if len(data) == 0 {
		writeErr(w, http.StatusBadRequest, "empty image")
		return
	}

	contentType := http.DetectContentType(data)
	if !allowedImageTypes[contentType] {
		writeErr(w, http.StatusBadRequest, "unsupported image type (use JPEG, PNG, GIF or WebP)")
		return
	}

	sum := sha256.Sum256(data)
	etag := hex.EncodeToString(sum[:])

	if _, err := a.DB.ExecContext(r.Context(),
		`INSERT INTO event_images (event_id, content_type, data, etag, updated_at)
		      VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (event_id) DO UPDATE
		      SET content_type = EXCLUDED.content_type,
		          data         = EXCLUDED.data,
		          etag         = EXCLUDED.etag,
		          updated_at   = now()`,
		id, contentType, data, etag); err != nil {
		metrics.EventMutationsTotal.WithLabelValues("image_upload", "error").Inc()
		serverErr(w, r, err, "db error")
		return
	}
	metrics.EventMutationsTotal.WithLabelValues("image_upload", "success").Inc()

	writeJSON(w, http.StatusOK, map[string]string{
		"imageUrl": eventImageURL(slug, sql.NullString{String: etag, Valid: true}),
	})
}

// handleDeleteEventImage removes an event's cover image, if any. Idempotent: a
// missing image still returns 204 so the admin UI can fire-and-forget.
func (a *App) handleDeleteEventImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := a.DB.ExecContext(r.Context(), `DELETE FROM event_images WHERE event_id = $1`, id); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleGetEventImage streams an event's cover image by slug. It is public (no
// auth) so a plain <img src> can load it, and it sets a strong ETag so browsers
// revalidate cheaply; the ?v= cache-buster on the URL makes replacements visible
// immediately despite the cache.
func (a *App) handleGetEventImage(w http.ResponseWriter, r *http.Request) {
	slug := strings.ToLower(chi.URLParam(r, "slug"))

	var contentType, etag string
	var data []byte
	err := a.DB.QueryRowContext(r.Context(),
		`SELECT i.content_type, i.data, i.etag
		   FROM event_images i JOIN events e ON e.id = i.event_id
		  WHERE e.slug = $1`, slug).Scan(&contentType, &data, &etag)
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "no image")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	quoted := `"` + etag + `"`
	w.Header().Set("ETag", quoted)
	w.Header().Set("Cache-Control", "public, max-age=300")
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, quoted) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
