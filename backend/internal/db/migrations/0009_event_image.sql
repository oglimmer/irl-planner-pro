-- 0009_event_image: optional cover image per event.
-- Stored out-of-line from the events row (1:1, PK = event_id) so the hot event
-- reads never pull the binary; the image is fetched only by its own endpoint.
-- etag is the content hash, used for HTTP caching / cache-busting on the URL.
CREATE TABLE IF NOT EXISTS event_images (
    event_id     UUID PRIMARY KEY REFERENCES events(id) ON DELETE CASCADE,
    content_type TEXT NOT NULL,
    data         BYTEA NOT NULL,
    etag         TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
