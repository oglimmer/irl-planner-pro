package server

import (
	"context"
	"database/sql"
	"os"
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
	if _, err := d.Exec(`TRUNCATE users, events, event_days, event_roster,
		submissions, submission_revisions, reminder_log, activity_log RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func testDBApp(t *testing.T) *App {
	return &App{
		Cfg: config.Config{JWTSecret: "test-secret-at-least-32-characters-long"},
		DB:  testDB(t),
	}
}

func TestFirstUserBecomesAdmin(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	first, err := a.findOrCreateUser(ctx, "Alice@id5.io", "Alice")
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if !first.IsAdmin {
		t.Error("first user should be admin")
	}
	if first.Email != "alice@id5.io" {
		t.Errorf("email not lower-cased: %q", first.Email)
	}

	second, err := a.findOrCreateUser(ctx, "bob@id5.io", "Bob")
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if second.IsAdmin {
		t.Error("second user should not be admin")
	}
}

func TestFindOrCreateUserIsIdempotentAndRefreshesName(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	u1, err := a.findOrCreateUser(ctx, "carol@id5.io", "Carol")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	u2, err := a.findOrCreateUser(ctx, "carol@id5.io", "Carol Smith")
	if err != nil {
		t.Fatalf("re-fetch: %v", err)
	}
	if u1.ID != u2.ID {
		t.Errorf("expected same user id, got %s vs %s", u1.ID, u2.ID)
	}
	if u2.Name != "Carol Smith" {
		t.Errorf("name not refreshed: %q", u2.Name)
	}
}

func TestCannotDemoteLastAdmin(t *testing.T) {
	a := testDBApp(t)
	ctx := context.Background()

	admin, _ := a.findOrCreateUser(ctx, "admin@id5.io", "Admin")

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
	other, _ := a.findOrCreateUser(ctx, "admin2@id5.io", "Admin2")
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
