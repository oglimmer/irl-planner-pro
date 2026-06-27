package slack

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigured(t *testing.T) {
	if (Notifier{}).Configured() {
		t.Fatal("zero Notifier should report not configured")
	}
	if (Notifier{Token: "   "}).Configured() {
		t.Fatal("blank token should report not configured")
	}
	if !(Notifier{Token: "xoxb-abc"}).Configured() {
		t.Fatal("token should report configured")
	}
}

func TestSendNotConfigured(t *testing.T) {
	if err := (Notifier{}).Send([]string{"a@id5.io"}, "s", "b"); err == nil {
		t.Fatal("expected error from unconfigured notifier")
	}
}

func TestSendLooksUpThenPosts(t *testing.T) {
	var gotAuth, gotLookupEmail, gotChannel, gotText string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/users.lookupByEmail"):
			gotLookupEmail = r.FormValue("email")
			io.WriteString(w, `{"ok":true,"user":{"id":"U123"}}`)
		case strings.HasSuffix(r.URL.Path, "/chat.postMessage"):
			gotChannel = r.FormValue("channel")
			gotText = r.FormValue("text")
			io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	n := Notifier{Token: "xoxb-test", APIBase: srv.URL}
	if err := n.Send([]string{"jane@id5.io"}, "You're invited", "Hello Jane"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAuth != "Bearer xoxb-test" {
		t.Errorf("Authorization = %q, want bearer token", gotAuth)
	}
	if gotLookupEmail != "jane@id5.io" {
		t.Errorf("lookup email = %q", gotLookupEmail)
	}
	if gotChannel != "U123" {
		t.Errorf("post channel = %q, want resolved user id U123", gotChannel)
	}
	// Subject becomes a bold first line above the body.
	if !strings.Contains(gotText, "*You're invited*") || !strings.Contains(gotText, "Hello Jane") {
		t.Errorf("post text = %q, want bold subject + body", gotText)
	}
}

func TestSendUserNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ok":false,"error":"users_not_found"}`)
	}))
	defer srv.Close()

	n := Notifier{Token: "xoxb-test", APIBase: srv.URL}
	err := n.Send([]string{"ghost@id5.io"}, "s", "b")
	if err == nil {
		t.Fatal("expected error when the email has no Slack user")
	}
	if !strings.Contains(err.Error(), "no Slack user") {
		t.Errorf("err = %v, want friendly users_not_found message", err)
	}
}

func TestSendPostErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/users.lookupByEmail") {
			io.WriteString(w, `{"ok":true,"user":{"id":"U9"}}`)
			return
		}
		io.WriteString(w, `{"ok":false,"error":"missing_scope"}`)
	}))
	defer srv.Close()

	n := Notifier{Token: "xoxb-test", APIBase: srv.URL}
	err := n.Send([]string{"jane@id5.io"}, "s", "b")
	if err == nil || !strings.Contains(err.Error(), "scope") {
		t.Fatalf("expected missing-scope error, got %v", err)
	}
}
