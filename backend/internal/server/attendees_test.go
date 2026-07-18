package server

import (
	"strings"
	"testing"
)

func TestParseRosterCSVWithHeader(t *testing.T) {
	csv := "Name,Email\nAlice Smith,Alice@oglimmer.com\nBob Jones,bob@oglimmer.com\n"
	entries, res := parseRosterCSV(strings.NewReader(csv))
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d (%+v)", len(entries), res)
	}
	if entries[0].Email != "alice@oglimmer.com" {
		t.Errorf("email should be lower-cased: %q", entries[0].Email)
	}
	if entries[0].FullName != "Alice Smith" {
		t.Errorf("name: %q", entries[0].FullName)
	}
}

func TestParseRosterCSVNoHeader(t *testing.T) {
	csv := "Alice Smith,alice@oglimmer.com\nBob Jones,bob@oglimmer.com\n"
	entries, _ := parseRosterCSV(strings.NewReader(csv))
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
}

func TestParseRosterCSVDedupAndInvalid(t *testing.T) {
	csv := "name,email\nAlice,alice@oglimmer.com\nAlice Again,ALICE@oglimmer.com\nNoEmail,\nBad,not-an-email\n"
	entries, res := parseRosterCSV(strings.NewReader(csv))
	if len(entries) != 1 {
		t.Fatalf("want 1 unique valid entry, got %d (%+v)", len(entries), entries)
	}
	if res.Skipped != 3 {
		t.Errorf("want 3 skipped (dup + missing + invalid), got %d", res.Skipped)
	}
}

func TestParseRosterCSVHeaderColumnOrder(t *testing.T) {
	// Columns in reversed order, named by header.
	csv := "email,name\nalice@oglimmer.com,Alice\n"
	entries, _ := parseRosterCSV(strings.NewReader(csv))
	if len(entries) != 1 || entries[0].FullName != "Alice" || entries[0].Email != "alice@oglimmer.com" {
		t.Fatalf("header column order not honoured: %+v", entries)
	}
}

func TestParseAttendingFilter(t *testing.T) {
	f := parseAttendingFilter("yes, no_response , bogus")
	if !f["yes"] || !f["no_response"] || f["bogus"] {
		t.Errorf("filter parse wrong: %+v", f)
	}
	if len(parseAttendingFilter("")) != 0 {
		t.Error("empty filter should be empty set")
	}
}
