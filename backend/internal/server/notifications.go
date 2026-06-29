package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Notification preferences are configured per event, per admin (the event
// editor's "Notifications" section). Each admin independently opts into one of
// two streams over one or both channels; the People team gets a daily summary
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
	PeopleTeamEmail        string          `json:"peopleTeamEmail"`        // the configured PEOPLE_TEAM_EMAIL ("" if unset)
	PeopleTeamDailySummary bool            `json:"peopleTeamDailySummary"` // events.daily_activity_email
	Channels               []channelStatus `json:"channels"`               // which transports are wired up
	Admins                 []adminNotifRow `json:"admins"`                 // every admin, left-joined to their prefs
}

// handleGetNotifications returns the per-event notification matrix: the
// People-team daily-summary toggle plus every admin left-joined to their stored
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
		  WHERE u.is_admin
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
		PeopleTeamEmail:        a.Cfg.PeopleTeamEmail,
		PeopleTeamDailySummary: e.DailyActivityEmail,
		Channels:               a.channelStatuses(),
		Admins:                 admins,
	})
}

type notificationsReq struct {
	PeopleTeamDailySummary bool `json:"peopleTeamDailySummary"`
	Admins                 []struct {
		UserID       string `json:"userId"`
		NotifType    string `json:"notifType"`
		ChannelEmail bool   `json:"viaEmail"`
		ChannelSlack bool   `json:"viaSlack"`
	} `json:"admins"`
}

// handleSaveNotifications replaces the whole matrix for an event in one tx:
// it sets the People-team daily-summary flag and rewrites every admin's
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

	tx, err := a.DB.BeginTx(r.Context(), nil)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(r.Context(),
		`UPDATE events SET daily_activity_email = $1, updated_at = now() WHERE id = $2`,
		req.PeopleTeamDailySummary, id)
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

	user := currentUser(r)
	if err := a.logActivity(r.Context(), a.DB, id, &user.ID, user.Email, "",
		actionNotificationsSaved, "Updated notification settings", nil, false); err != nil {
		log.Printf("WARN: log notifications save for %s: %v", id, err)
	}
	a.handleGetNotifications(w, r)
}
