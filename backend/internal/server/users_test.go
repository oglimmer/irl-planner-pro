package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"irlplanner/internal/config"
	"irlplanner/internal/db"
)

// testDB opens the database named by IRL_TEST_DATABASE_URL, applies migrations,
// and truncates every table so each test starts clean. The whole suite is
// skipped when the env var is unset, so `go test ./...` stays green without a
// database while CI can run the integration path against a real Postgres.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("IRL_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set IRL_TEST_DATABASE_URL to run DB-backed tests")
	}
	d, err := db.Open(url)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := d.Exec(`TRUNCATE users, events, event_days, event_roster, event_attendees, event_images,
		submissions, submission_revisions, reminder_log, activity_log,
		oauth_auth_codes, oauth_refresh_tokens, oauth_pending RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func testDBApp(t *testing.T) *App {
	d := testDB(t)
	return &App{
		Cfg:   config.Config{JWTSecret: "test-secret-at-least-32-characters-long"},
		DB:    d,
		Store: NewStore(d),
	}
}

func TestFirstUserBecomesAdmin(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	first, err := a.Store.findOrCreateUser(ctx, "Alice@id5.io", "Alice", "", "")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if !first.IsAdmin {
		t.Error("first user should be admin")
	}
	if first.Email != "alice@id5.io" {
		t.Errorf("email not lower-cased: %q", first.Email)
	}

	second, err := a.Store.findOrCreateUser(ctx, "bob@id5.io", "Bob", "", "")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.IsAdmin {
		t.Error("second user should not be admin")
	}
}

// The IdP seeds the name only on first login; a later login must not overwrite
// what the user (or that first login) already set, so a profile edit always wins.
func TestFindOrCreateUserSeedsNameOnceAndIsIdempotent(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	u1, err := a.Store.findOrCreateUser(ctx, "carol@id5.io", "Carol", "Jones", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u1.FirstName != "Carol" || u1.LastName != "Jones" || u1.Name != "Carol Jones" {
		t.Errorf("unexpected seeded name: %q / %q (%q)", u1.FirstName, u1.LastName, u1.Name)
	}

	// Second login with a different IdP name: same user, name unchanged.
	u2, err := a.Store.findOrCreateUser(ctx, "carol@id5.io", "Caroline", "Smith", "")
	if err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	if u1.ID != u2.ID {
		t.Errorf("expected same user id, got %s vs %s", u1.ID, u2.ID)
	}
	if u2.FirstName != "Carol" || u2.LastName != "Jones" {
		t.Errorf("name should not be refreshed from the IdP: %q / %q", u2.FirstName, u2.LastName)
	}
}

// handleUpdateMe lets the user change their own name; a subsequent login keeps it.
func TestHandleUpdateMe(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	u, err := a.Store.findOrCreateUser(ctx, "dave@id5.io", "Dave", "Initial", "peanuts")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Allergies given at first login are seeded onto the profile.
	if u.Allergies != "peanuts" {
		t.Errorf("allergies not seeded on create: %q", u.Allergies)
	}
	// A freshly provisioned user hasn't confirmed their profile yet, so the SPA
	// routes them through the first-login confirm step.
	if u.ProfileConfirmed {
		t.Error("new user should start with profileConfirmed = false")
	}

	// Empty name is rejected.
	if rr := a.doUpdateMe(t, u, `{"firstName":"  ","lastName":"x"}`); rr.Code != 400 {
		t.Errorf("blank first name: want 400, got %d", rr.Code)
	}

	// Happy path: name + allergies are editable (allergies may also be cleared).
	rr := a.doUpdateMe(t, u, `{"firstName":"David","lastName":"Edited","allergies":"shellfish"}`)
	if rr.Code != 200 {
		t.Fatalf("update: want 200, got %d (%s)", rr.Code, rr.Body.String())
	}
	reloaded, _ := a.Store.userByID(ctx, u.ID)
	if reloaded.FirstName != "David" || reloaded.LastName != "Edited" || reloaded.Name != "David Edited" {
		t.Errorf("edit not persisted: %q / %q (%q)", reloaded.FirstName, reloaded.LastName, reloaded.Name)
	}
	if reloaded.Allergies != "shellfish" {
		t.Errorf("allergies edit not persisted: %q", reloaded.Allergies)
	}
	// Saving the profile is what marks it confirmed (the first-login confirm step).
	if !reloaded.ProfileConfirmed {
		t.Error("profileConfirmed should be true after PUT /api/me")
	}

	// A later login does not clobber the edits (first-login-only seeding) — not
	// the name, nor the allergies.
	again, _ := a.Store.findOrCreateUser(ctx, "dave@id5.io", "Dave", "Initial", "gluten")
	if again.FirstName != "David" || again.LastName != "Edited" {
		t.Errorf("login overwrote the profile edit: %q / %q", again.FirstName, again.LastName)
	}
	if again.Allergies != "shellfish" {
		t.Errorf("login overwrote the allergies edit: %q", again.Allergies)
	}
}

// doUpdateMe drives handleUpdateMe with user stashed in context, returning the recorder.
func (a *App) doUpdateMe(t *testing.T, user *User, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut, "/api/me", strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), ctxUserKey, user))
	rr := httptest.NewRecorder()
	a.handleUpdateMe(rr, r)
	return rr
}

func TestCannotDemoteLastAdmin(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	admin, _ := a.Store.findOrCreateUser(ctx, "admin@id5.io", "Admin", "", "")

	// The single admin cannot be demoted (guard via the EXISTS subquery).
	res, err := a.DB.ExecContext(ctx,
		`UPDATE users SET is_admin = false
		 WHERE id = $1 AND EXISTS (SELECT 1 FROM users WHERE is_admin AND id <> $1)`, admin.ID)
	if err != nil {
		t.Fatalf("demote: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 0 {
		t.Fatal("last admin should not be demotable")
	}

	// With a second admin present, the first can be demoted.
	other, _ := a.Store.findOrCreateUser(ctx, "admin2@id5.io", "Admin2", "", "")
	a.DB.ExecContext(ctx, `UPDATE users SET is_admin = true WHERE id = $1`, other.ID)
	res, err = a.DB.ExecContext(ctx,
		`UPDATE users SET is_admin = false
		 WHERE id = $1 AND EXISTS (SELECT 1 FROM users WHERE is_admin AND id <> $1)`, admin.ID)
	if err != nil {
		t.Fatalf("demote with 2 admins: %v", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatal("should be able to demote when another admin exists")
	}
}
