package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Notification preferences are configured per event, per admin (the event
// editor's "Notifications" section). Each admin independently opts into one of
// two streams over one or both channels; the IRL team gets a daily summary
// by email only, gated by the event-level flag. See DESIGN.md §9.
//
// The wire/storage values of event_admin_notifications.notif_type.
const (
	notifTypeDaily    = "daily"    // the once-a-day activity digest
	notifTypeActivity = "activity" // an immediate alert on every submission write
)

// notifyTargets returns the opted-in admins for one notification stream on an
// event, split by delivery channel (an admin can choose email, slack, or both).
// Both slices hold company email addresses — the Slack notifier resolves each
// to a workspace user. Best-effort: a query error logs a WARN and returns empty
// lists so a notification path degrades to "nobody" rather than failing.
func (a *App) notifyTargets(ctx context.Context, eventID, notifType string) (emailTo, slackTo []string) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT u.email, n.channel_email, n.channel_slack
		   FROM event_admin_notifications n
		   JOIN users u ON u.id = n.user_id
		  WHERE n.event_id = $1 AND n.notif_type = $2
		  ORDER BY u.email`, eventID, notifType)
	if err != nil {
		log.Printf("WARN: notifyTargets %s/%s: %v", eventID, notifType, err)
		return nil, nil
	}
	defer rows.Close()
	for rows.Next() {
		var email string
		var ce, cs bool
		if err := rows.Scan(&email, &ce, &cs); err != nil {
			log.Printf("WARN: notifyTargets scan: %v", err)
			continue
		}
		if ce {
			emailTo = append(emailTo, email)
		}
		if cs {
			slackTo = append(slackTo, email)
		}
	}
	return emailTo, slackTo
}

// dispatch delivers one message to the per-channel recipient lists, skipping a
// channel that has no recipients or whose transport isn't configured. Returns
// the number of channels actually sent on. Best-effort: a send failure logs a
// WARN and never propagates (notifications must never break a request).
func (a *App) dispatch(emailTo, slackTo []string, subject, body string) int {
	sent := 0
	if len(emailTo) > 0 && a.Email.Configured() {
		if err := a.Email.Send(emailTo, subject, body); err != nil {
			log.Printf("WARN: notification email failed: %v", err)
		} else {
			sent++
		}
	}
	if len(slackTo) > 0 && a.Slack.Configured() {
		if err := a.Slack.Send(slackTo, subject, body); err != nil {
			log.Printf("WARN: notification slack failed: %v", err)
		} else {
			sent++
		}
	}
	return sent
}

// handleSendTestNotification sends a one-off test message over a single channel
// ("email" or "slack") to the currently logged-in admin. It's a quick way to
// confirm a transport is wired up end-to-end (SMTP creds, Slack bot token +
// email→user resolution) without waiting for a real submission or digest.
func (a *App) handleSendTestNotification(w http.ResponseWriter, r *http.Request) {
	channel := chi.URLParam(r, "channel")
	user := currentUser(r)
	subject := "[IRL] Test notification"
	body := "This is a test notification from the IRL Planner.\n\n" +
		"If you received this, the channel is configured correctly.\n"

	var err error
	switch channel {
	case channelEmail:
		if !a.Email.Configured() {
			writeErr(w, http.StatusBadRequest, "email is not configured on this server")
			return
		}
		err = a.Email.Send([]string{user.Email}, subject, body)
	case channelSlack:
		if !a.Slack.Configured() {
			writeErr(w, http.StatusBadRequest, "slack is not configured on this server")
			return
		}
		err = a.Slack.Send([]string{user.Email}, subject, body)
	default:
		writeErr(w, http.StatusBadRequest, "channel must be 'email' or 'slack'")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, "failed to send test "+channel+": "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "to": user.Email})
}

// --- handlers --------------------------------------------------------------

// adminNotifRow is one row of the per-event admin matrix: an admin and their
// chosen stream + channels. NotifType is "" (off), "daily", or "activity".
type adminNotifRow struct {
	UserID       string `json:"userId"`
	Name         string `json:"name"`
	Email        string `json:"email"` // the admin's address (display + the value sent to a channel)
	NotifType    string `json:"notifType"`
	ChannelEmail bool   `json:"viaEmail"`
	ChannelSlack bool   `json:"viaSlack"`
}

type notificationsResp struct {
	IRLTeamEmail        string          `json:"irlTeamEmail"`        // the configured IRL_TEAM_EMAIL ("" if unset)
	IRLTeamDailySummary bool            `json:"irlTeamDailySummary"` // events.daily_activity_email
	Channels            []channelStatus `json:"channels"`            // which transports are wired up
	Admins              []adminNotifRow `json:"admins"`              // every admin, left-joined to their prefs
}

// handleGetNotifications returns the per-event notification matrix: the
// IRL-team daily-summary toggle plus every admin left-joined to their stored
// preference (admins with no row show as "off").
func (a *App) handleGetNotifications(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	e, err := a.Store.loadEventByColumn(r.Context(), "id", id, time.Now())
	if err == sql.ErrNoRows {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT u.id, u.email, u.first_name, u.last_name,
		        COALESCE(n.notif_type, ''), COALESCE(n.channel_email, false), COALESCE(n.channel_slack, false)
		   FROM users u
		   LEFT JOIN event_admin_notifications n ON n.user_id = u.id AND n.event_id = $1
		  WHERE u.is_admin AND NOT u.archived
		  ORDER BY u.created_at`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()
	admins := []adminNotifRow{}
	for rows.Next() {
		var row adminNotifRow
		var first, last string
		if err := rows.Scan(&row.UserID, &row.Email, &first, &last, &row.NotifType, &row.ChannelEmail, &row.ChannelSlack); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		row.Name = strings.TrimSpace(first + " " + last)
		admins = append(admins, row)
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	writeJSON(w, http.StatusOK, notificationsResp{
		IRLTeamEmail:        a.Cfg.IRLTeamEmail,
		IRLTeamDailySummary: e.DailyActivityEmail,
		Channels:            a.channelStatuses(),
		Admins:              admins,
	})
}

type notificationsReq struct {
	IRLTeamDailySummary bool `json:"irlTeamDailySummary"`
	Admins              []struct {
		UserID       string `json:"userId"`
		NotifType    string `json:"notifType"`
		ChannelEmail bool   `json:"viaEmail"`
		ChannelSlack bool   `json:"viaSlack"`
	} `json:"admins"`
}

// handleSaveNotifications replaces the whole matrix for an event in one tx:
// it sets the IRL-team daily-summary flag and rewrites every admin's
// preference (a full replace — rows for "off" admins are simply omitted, so
// any prior row is dropped). Validates the stream and that a live row picks at
// least one channel.
func (a *App) handleSaveNotifications(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req notificationsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Validate before touching the DB so a bad row rejects the whole save.
	for _, row := range req.Admins {
		switch row.NotifType {
		case "", notifTypeDaily, notifTypeActivity:
		default:
			writeErr(w, http.StatusBadRequest, "notifType must be '', 'daily', or 'activity'")
			return
		}
		if row.NotifType != "" && !row.ChannelEmail && !row.ChannelSlack {
			writeErr(w, http.StatusBadRequest, "an admin with a notification stream must select at least one channel")
			return
		}
	}

	// Capture old state inside the transaction so the diff is consistent.
	type oldPref struct {
		Email        string
		NotifType    string
		ChannelEmail bool
		ChannelSlack bool
	}
	oldByUser := map[string]oldPref{}

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	// Read current notification rows (joined with users for the email).
	rows, err := tx.QueryContext(r.Context(),
		`SELECT n.user_id, u.email, n.notif_type, n.channel_email, n.channel_slack
		   FROM event_admin_notifications n
		   JOIN users u ON u.id = n.user_id
		  WHERE n.event_id = $1`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	for rows.Next() {
		var uid string
		var op oldPref
		if err := rows.Scan(&uid, &op.Email, &op.NotifType, &op.ChannelEmail, &op.ChannelSlack); err != nil {
			rows.Close()
			serverErr(w, r, err, "db error")
			return
		}
		oldByUser[uid] = op
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	// Old daily-activity-email flag.
	var oldDAE bool
	if err := tx.QueryRowContext(r.Context(),
		`SELECT daily_activity_email FROM events WHERE id = $1`, id).Scan(&oldDAE); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	// Build the list of changes for the activity log.
	changes := []ActivityChange{}
	if oldDAE != req.IRLTeamDailySummary {
		fromS := "off"
		if oldDAE {
			fromS = "on"
		}
		toS := "off"
		if req.IRLTeamDailySummary {
			toS = "on"
		}
		changes = append(changes, ActivityChange{Field: "IRL team daily summary", From: fromS, To: toS})
	}

	res, err := tx.ExecContext(r.Context(),
		`UPDATE events SET daily_activity_email = $1, updated_at = now() WHERE id = $2`,
		req.IRLTeamDailySummary, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}

	// Full replace: clear the event's matrix, then insert only the live rows.
	if _, err := tx.ExecContext(r.Context(),
		`DELETE FROM event_admin_notifications WHERE event_id = $1`, id); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	for _, row := range req.Admins {
		if row.NotifType == "" {
			continue // "off" — no row
		}
		if _, err := tx.ExecContext(r.Context(),
			`INSERT INTO event_admin_notifications (event_id, user_id, notif_type, channel_email, channel_slack)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (event_id, user_id) DO UPDATE
			    SET notif_type = EXCLUDED.notif_type,
			        channel_email = EXCLUDED.channel_email,
			        channel_slack = EXCLUDED.channel_slack`,
			id, row.UserID, row.NotifType, row.ChannelEmail, row.ChannelSlack); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	// Complete the diff for each requested admin *after* the commit, using the
	// old state we captured.
	for _, row := range req.Admins {
		old, existed := oldByUser[row.UserID]
		email := ""
		if existed {
			email = old.Email
		} else {
			// The admin may not have had a row before; get the email from users.
			if err := a.DB.QueryRowContext(r.Context(),
				`SELECT email FROM users WHERE id = $1`, row.UserID).Scan(&email); err != nil {
				log.Printf("WARN: notifications activity diff: user %s: %v", row.UserID, err)
				continue
			}
		}
		oldType := ""
		oldEmailCh := false
		oldSlackCh := false
		if existed {
			oldType = old.NotifType
			oldEmailCh = old.ChannelEmail
			oldSlackCh = old.ChannelSlack
		}
		if oldType == row.NotifType && oldEmailCh == row.ChannelEmail && oldSlackCh == row.ChannelSlack {
			continue // no change for this admin
		}
		fromS := "off"
		if oldType != "" {
			fromS = fmt.Sprintf("%s (email=%t, slack=%t)", oldType, oldEmailCh, oldSlackCh)
		}
		toS := "off"
		if row.NotifType != "" {
			toS = fmt.Sprintf("%s (email=%t, slack=%t)", row.NotifType, row.ChannelEmail, row.ChannelSlack)
		}
		changes = append(changes, ActivityChange{
			Field: fmt.Sprintf("Admin %s", email),
			From:  fromS,
			To:    toS,
		})
	}

	summary := "Updated notification settings"
	if len(changes) > 0 {
		summary += fmt.Sprintf(" (%d change(s))", len(changes))
	}

	user := currentUser(r)
	a.logActivity(r.Context(), a.DB, id, &user.ID, user.Email, "",
		actionNotificationsSaved, summary,
		map[string]any{"changes": changes}, false)
	a.handleGetNotifications(w, r)
}
