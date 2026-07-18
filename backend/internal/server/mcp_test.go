package server

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// resultText concatenates the text of every TextContent block in a tool result.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("unexpected content type %T, want *mcp.TextContent", c)
		}
		b.WriteString(tc.Text)
	}
	return b.String()
}

// TestMCPHandlerRegistersTools is a smoke test: building the handler registers
// every tool, and the SDK validates each tool's input schema (derived from the
// jsonschema struct tags) at registration. A malformed tag would panic here.
func TestMCPHandlerRegistersTools(t *testing.T) {
	a := oauthTestApp()
	if h := a.mcpHandler(); h == nil {
		t.Fatal("mcpHandler returned nil")
	}
}

// TestOKResultEmbedsPayloadInText pins the property that read tools render the
// full payload into the text Content (not only into StructuredContent, which
// some MCP clients ignore): the summary header followed by the JSON body.
func TestOKResultEmbedsPayloadInText(t *testing.T) {
	out := mcpListEventsOut{Events: []mcpEventSummary{
		{Slug: "dubrovnik-oct-2026", Name: "IRL Dubrovnik October 2026", Country: "Croatia", Responses: 33, RosterTotal: 48},
		{Slug: "lisbon-mar-2027", Name: "IRL Lisbon March 2027", Country: "Portugal"},
	}}

	res, gotOut, err := okResult("2 event(s)", out)
	if err != nil {
		t.Fatalf("okResult returned error: %v", err)
	}
	// The structured value must pass through untouched.
	if len(gotOut.Events) != 2 || gotOut.Events[0].Slug != "dubrovnik-oct-2026" {
		t.Fatalf("okResult mutated its out value: %+v", gotOut)
	}

	text := resultText(t, res)
	if !strings.Contains(text, "2 event(s)") {
		t.Errorf("text content missing summary header; got:\n%s", text)
	}
	for _, want := range []string{
		"dubrovnik-oct-2026", "IRL Dubrovnik October 2026", "Croatia",
		"lisbon-mar-2027", "IRL Lisbon March 2027",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("text content missing %q; got:\n%s", want, text)
		}
	}
}

// TestRequireMCPAdmin enforces the gate every tool runs first: unauthenticated
// callers and non-admins are rejected before any data is touched.
func TestRequireMCPAdmin(t *testing.T) {
	// No user in context → unauthenticated.
	if _, err := requireMCPAdmin(context.Background()); err == nil {
		t.Error("requireMCPAdmin allowed an unauthenticated caller")
	}

	// Non-admin user → admin only, and the error names the user tools so an MCP
	// client can self-correct.
	nonAdmin := context.WithValue(context.Background(), ctxUserKey, &User{ID: "u1", Email: "e@oglimmer.com", IsAdmin: false})
	err := func() error { _, e := requireMCPAdmin(nonAdmin); return e }()
	if err == nil {
		t.Fatal("requireMCPAdmin allowed a non-admin caller")
	}
	for _, want := range userToolNames {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("admin-only error doesn't mention user tool %q; got: %v", want, err)
		}
	}

	// Admin user → allowed.
	admin := context.WithValue(context.Background(), ctxUserKey, &User{ID: "u2", Email: "a@oglimmer.com", IsAdmin: true})
	u, err := requireMCPAdmin(admin)
	if err != nil {
		t.Fatalf("requireMCPAdmin rejected an admin: %v", err)
	}
	if u == nil || u.ID != "u2" {
		t.Errorf("requireMCPAdmin returned wrong user: %+v", u)
	}
}

// TestRequireMCPUser checks the user-tool gate: only an unauthenticated caller is
// rejected; both regular users and admins are admitted (admins are users too).
func TestRequireMCPUser(t *testing.T) {
	if _, err := requireMCPUser(context.Background()); err == nil {
		t.Error("requireMCPUser allowed an unauthenticated caller")
	}
	for _, isAdmin := range []bool{false, true} {
		ctx := context.WithValue(context.Background(), ctxUserKey, &User{ID: "u", Email: "e@oglimmer.com", IsAdmin: isAdmin})
		if _, err := requireMCPUser(ctx); err != nil {
			t.Errorf("requireMCPUser rejected a signed-in user (isAdmin=%v): %v", isAdmin, err)
		}
	}
}

// TestProfileOut derives the display name from the two name parts and carries the
// confirm flag through.
func TestProfileOut(t *testing.T) {
	out := profileOut(&User{Email: "a@oglimmer.com", FirstName: "Ada", LastName: "Lovelace", Allergies: "none", ProfileConfirmed: true})
	if out.Name != "Ada Lovelace" {
		t.Errorf("Name = %q, want %q", out.Name, "Ada Lovelace")
	}
	if !out.ProfileConfirmed || out.Allergies != "none" {
		t.Errorf("profileOut dropped fields: %+v", out)
	}
}

// TestResolveEventRefEmpty checks the friendly error for a blank reference,
// which returns before any DB access.
func TestResolveEventRefEmpty(t *testing.T) {
	a := oauthTestApp()
	if _, err := a.resolveEventRef(context.Background(), "  "); err == nil {
		t.Error("resolveEventRef accepted an empty reference")
	}
}
