package server

import (
	"testing"

	"irlplanner/internal/config"
)

func testApp() *App {
	return &App{Cfg: config.Config{JWTSecret: "test-secret-at-least-32-characters-long"}}
}

func TestIssueParseTokenRoundtrip(t *testing.T) {
	a := testApp()
	tok, err := a.issueToken("user-123", 7)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	sub, typ, ver, err := a.parseToken(tok)
	if err != nil {
		t.Fatalf("parseToken: %v", err)
	}
	if sub != "user-123" {
		t.Errorf("sub = %q, want user-123", sub)
	}
	if typ != "" {
		t.Errorf("typ = %q, want empty for a session token", typ)
	}
	if ver != 7 {
		t.Errorf("ver = %d, want 7", ver)
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	a := testApp()
	tok, _ := a.issueToken("u", 0)
	other := &App{Cfg: config.Config{JWTSecret: "a-completely-different-secret-32chars"}}
	if _, _, _, err := other.parseToken(tok); err == nil {
		t.Fatal("expected error parsing a token signed with a different secret")
	}
}

func TestParseTokenRejectsGarbage(t *testing.T) {
	a := testApp()
	if _, _, _, err := a.parseToken("not.a.jwt"); err == nil {
		t.Fatal("expected error parsing garbage")
	}
}
