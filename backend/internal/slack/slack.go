// Package slack is a stub Slack notifier. It deliberately mirrors the shape of
// email.Sender (Configured + Send) so the messaging dispatcher can treat Slack
// as just another channel. Real delivery — a bot token, the chat.postMessage
// API, and a mapping from company email to Slack user ID — is not implemented
// yet; Send is a logged no-op and Configured always reports false, so the UI can
// surface Slack as "coming soon" and the backend never claims to have sent.
package slack

import "log"

// Notifier holds Slack connection settings. Today it carries none: the zero
// value is the only value, and it is always "not configured".
type Notifier struct{}

// Configured reports whether Slack delivery is wired up. Always false until the
// real client lands (see package doc).
func (Notifier) Configured() bool { return false }

// Send is a no-op stub. It logs the intent at WARN and returns nil so callers
// that dispatch to Slack don't fail — they simply deliver nothing yet. The
// signature matches email.Sender.Send so the dispatcher stays uniform.
func (Notifier) Send(to []string, subject, _ string) error {
	log.Printf("WARN: slack messaging not implemented — dropping message %q to %d recipient(s)", subject, len(to))
	return nil
}
