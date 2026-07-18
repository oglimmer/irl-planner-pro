package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// mkAdmin creates a user and ensures they are an admin, returning the id.
func mkAdmin(t *testing.T, a *App, ctx context.Context, email string) string {
	t.Helper()
	u, err := a.Store.findOrCreateUser(ctx, email, "Test", "", "")
	if err != nil {
		t.Fatalf("create %s: %v", email, err)
	}
	if !u.IsAdmin {
		if _, err := a.DB.ExecContext(ctx, `UPDATE users SET is_admin = true WHERE id = $1`, u.ID); err != nil {
			t.Fatalf("promote %s: %v", email, err)
		}
	}
	return u.ID
}

// notifyTargets splits opted-in admins by the channel(s) they selected.
func TestNotifyTargetsSplitsByChannel(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	admin1 := mkAdmin(t, a, ctx, "first@oglimmer.com")  // both channels, daily
	admin2 := mkAdmin(t, a, ctx, "second@oglimmer.com") // email only, activity
	eventID := mkEventForTest(t, a, ctx, admin1, "notif-event", "2026-09-01", "2026-09-03")

	ins := func(userID, typ string, email, slack bool) {
		if _, err := a.DB.ExecContext(ctx,
			`INSERT INTO event_admin_notifications (event_id, user_id, notif_type, channel_email, channel_slack)
			 VALUES ($1,$2,$3,$4,$5)`, eventID, userID, typ, email, slack); err != nil {
			t.Fatalf("insert pref: %v", err)
		}
	}
	ins(admin1, notifTypeDaily, true, true)
	ins(admin2, notifTypeActivity, true, false)

	dailyEmail, dailySlack := a.notifyTargets(ctx, eventID, notifTypeDaily)
	if len(dailyEmail) != 1 || dailyEmail[0] != "first@oglimmer.com" {
		t.Errorf("daily email = %v, want [first@oglimmer.com]", dailyEmail)
	}
	if len(dailySlack) != 1 || dailySlack[0] != "first@oglimmer.com" {
		t.Errorf("daily slack = %v, want [first@oglimmer.com]", dailySlack)
	}

	actEmail, actSlack := a.notifyTargets(ctx, eventID, notifTypeActivity)
	if len(actEmail) != 1 || actEmail[0] != "second@oglimmer.com" {
		t.Errorf("activity email = %v, want [second@oglimmer.com]", actEmail)
	}
	if len(actSlack) != 0 {
		t.Errorf("activity slack = %v, want []", actSlack)
	}
}

// putNotifications drives the save handler with the id chi-param wired up.
func putNotifications(t *testing.T, a *App, ctx context.Context, adminID, eventID string, body notificationsReq) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPut, "/api/admin/events/"+eventID+"/notifications", bytes.NewReader(raw))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", eventID)
	r = r.WithContext(context.WithValue(withAdmin(ctx, adminID), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	a.handleSaveNotifications(w, r)
	return w
}

// The save handler persists the IRL team flag and a full-replace of the
// admin matrix, and rejects a stream with no channel.
func TestSaveNotificationsHandler(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()
	admin := mkAdmin(t, a, ctx, "admin@oglimmer.com")
	eventID := mkEventForTest(t, a, ctx, admin, "save-event", "2026-09-01", "2026-09-03")

	row := func(typ string, email, slack bool) struct {
		UserID       string `json:"userId"`
		NotifType    string `json:"notifType"`
		ChannelEmail bool   `json:"viaEmail"`
		ChannelSlack bool   `json:"viaSlack"`
	} {
		return struct {
			UserID       string `json:"userId"`
			NotifType    string `json:"notifType"`
			ChannelEmail bool   `json:"viaEmail"`
			ChannelSlack bool   `json:"viaSlack"`
		}{admin, typ, email, slack}
	}

	// A stream with no channel is rejected.
	bad := notificationsReq{IRLTeamDailySummary: true}
	bad.Admins = append(bad.Admins, row(notifTypeActivity, false, false))
	if w := putNotifications(t, a, ctx, admin, eventID, bad); w.Code != http.StatusBadRequest {
		t.Fatalf("no-channel stream: status %d, want 400 (body %s)", w.Code, w.Body.String())
	}

	// A valid save persists the flag and the row.
	good := notificationsReq{IRLTeamDailySummary: true}
	good.Admins = append(good.Admins, row(notifTypeActivity, true, false))
	if w := putNotifications(t, a, ctx, admin, eventID, good); w.Code != http.StatusOK {
		t.Fatalf("valid save: status %d (body %s)", w.Code, w.Body.String())
	}

	var dae bool
	if err := a.DB.QueryRowContext(ctx, `SELECT daily_activity_email FROM events WHERE id=$1`, eventID).Scan(&dae); err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if !dae {
		t.Error("daily_activity_email should be true after save")
	}
	email, slack := a.notifyTargets(ctx, eventID, notifTypeActivity)
	if len(email) != 1 || len(slack) != 0 {
		t.Errorf("after save: email=%v slack=%v, want 1 email / 0 slack", email, slack)
	}

	// Setting the admin to "off" (omitted/empty type) clears the row.
	off := notificationsReq{IRLTeamDailySummary: false}
	off.Admins = append(off.Admins, row("", false, false))
	if w := putNotifications(t, a, ctx, admin, eventID, off); w.Code != http.StatusOK {
		t.Fatalf("off save: status %d (body %s)", w.Code, w.Body.String())
	}
	email, slack = a.notifyTargets(ctx, eventID, notifTypeActivity)
	if len(email) != 0 || len(slack) != 0 {
		t.Errorf("after off: email=%v slack=%v, want empty", email, slack)
	}
}
