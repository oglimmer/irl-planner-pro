package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"irlplanner/internal/metrics"
)

// Phase 7 MCP server (DESIGN.md §18). An additive, OAuth-gated surface that lets
// an MCP client (e.g. Claude) query and manage events. It is a thin protocol
// adapter over the existing query/command functions in events.go, dashboard.go,
// roster.go, activity.go, and reminders.go — not a second copy of the business
// logic. Off unless MCP_OAUTH_CLIENT_* are configured.
//
// Two tiers (DESIGN.md §18.1): the admin tools (all the dashboards, exports, and
// event/roster mutations) require an admin, exposing nothing a non-admin couldn't
// already reach through the SPA admin UI. The user tools (userToolNames) require
// only a signed-in employee and act on that caller themselves — enough for a
// regular user to see the active events, manage their own profile, and RSVP
// entirely over MCP. The full tool list is advertised to everyone; a non-admin
// who reaches for an admin tool gets an error naming the tools they *can* call.

// uuidRe matches a canonical UUID, used to decide whether an event reference
// should be looked up by id (vs. slug) without provoking a Postgres
// "invalid input syntax for type uuid" error.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// userFromCtx pulls the *User stashed by mcpTokenGateMiddleware. Tool handlers
// run with the HTTP request context, so the value is reachable here.
func userFromCtx(ctx context.Context) *User {
	v, _ := ctx.Value(ctxUserKey).(*User)
	return v
}

// userToolNames are the tools any signed-in employee may call. Kept as data so
// requireMCPAdmin can name them when guiding a non-admin away from an admin tool.
var userToolNames = []string{
	"list_events", "get_event", "submit_response", "get_profile", "update_profile",
}

// requireMCPUser enforces only that the caller is authenticated. The user tools
// all act on the caller themselves (their own profile / RSVP / the active
// events), so any signed-in employee — admins included — may call them.
func requireMCPUser(ctx context.Context) (*User, error) {
	u := userFromCtx(ctx)
	if u == nil {
		return nil, errors.New("unauthenticated")
	}
	return u, nil
}

// requireMCPAdmin enforces the same authorization as the REST admin group: the
// caller must be authenticated and an admin. The admin tools expose nothing a
// non-admin couldn't reach through the SPA admin UI (DESIGN.md §18.1). On refusal
// it names the user tools so an MCP client can self-correct.
func requireMCPAdmin(ctx context.Context) (*User, error) {
	u := userFromCtx(ctx)
	if u == nil {
		return nil, errors.New("unauthenticated")
	}
	if !u.IsAdmin {
		return nil, fmt.Errorf("this tool is admin-only; as a regular user you can call: %s",
			strings.Join(userToolNames, ", "))
	}
	return u, nil
}

func (a *App) mcpHandler() http.Handler {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    "irl-planner-pro",
			Title:   "IRL Attendance",
			Version: "0.1.0",
		},
		nil,
	)
	a.registerMCPTools(server)
	// Stateless: no session map kept across requests. Every tool is a stateless
	// read/write against Postgres, so there is nothing worth preserving per
	// session — and stateful mode would otherwise return 404 "session not found"
	// to every client whose session ID predates the last backend restart.
	return mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true},
	)
}

// --- output shapes ---------------------------------------------------------

type mcpEventSummary struct {
	ID                 string    `json:"id"`
	Slug               string    `json:"slug"`
	Name               string    `json:"name"`
	Country            string    `json:"country"`
	City               string    `json:"city"`
	Timezone           string    `json:"timezone"`
	StartDate          string    `json:"startDate"`
	EndDate            string    `json:"endDate"`
	SubmissionDeadline time.Time `json:"submissionDeadline"`
	IsPast             bool      `json:"isPast"`
	Responses          int       `json:"responses"`   // submissions received
	RosterTotal        int       `json:"rosterTotal"` // people on the attendee list
}

type mcpListEventsOut struct {
	Events []mcpEventSummary `json:"events"`
}

// mcpMyEventSummary is the regular-user view of an active event: its public
// details plus the caller's own RSVP. It deliberately omits the response/roster
// counts (other people's data) that the admin mcpEventSummary carries.
type mcpMyEventSummary struct {
	ID                 string    `json:"id"`
	Slug               string    `json:"slug"`
	Name               string    `json:"name"`
	Country            string    `json:"country"`
	City               string    `json:"city"`
	Timezone           string    `json:"timezone"`
	StartDate          string    `json:"startDate"`
	EndDate            string    `json:"endDate"`
	SubmissionDeadline time.Time `json:"submissionDeadline"`
	HasSubmitted       bool      `json:"hasSubmitted"`
	MyAttending        string    `json:"myAttending"` // yes|no|not_sure, or "" if not submitted
}

type mcpMyEventsOut struct {
	Events []mcpMyEventSummary `json:"events"`
}

type mcpNonResponder struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
}

type mcpNonRespondersOut struct {
	Event         string            `json:"event"`
	NonResponders []mcpNonResponder `json:"nonResponders"`
}

type mcpSubmissionsOut struct {
	Event       string       `json:"event"`
	Submissions []Submission `json:"submissions"`
}

type mcpActivityOut struct {
	Event    string          `json:"event"`
	Activity []ActivityEntry `json:"activity"`
}

type mcpStatusOut struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// --- input shapes ----------------------------------------------------------

type mcpEmptyIn struct{}

type mcpEventRefIn struct {
	Event string `json:"event" jsonschema:"the event slug (e.g. dubrovnik-oct-2026) or its id"`
}

// mcpEventRefOptIn is the user-tool event reference: optional, defaulting to the
// sole active event when omitted.
type mcpEventRefOptIn struct {
	Event string `json:"event,omitempty" jsonschema:"the event slug or id; omit to use the sole active event (errors if there are several)"`
}

type mcpGetActivityIn struct {
	Event    string `json:"event" jsonschema:"the event slug or id"`
	Category string `json:"category,omitempty" jsonschema:"optional filter: 'user' for participant actions (submissions) or 'admin' for administrative actions (event config, roster, reminders); empty means all"`
}

type mcpListSubmissionsIn struct {
	Event     string `json:"event" jsonschema:"the event slug or id"`
	Attending string `json:"attending,omitempty" jsonschema:"optional comma-separated filter over yes,no,not_sure,no_response; empty means all"`
}

type mcpCreateEventIn struct {
	Slug                    string `json:"slug" jsonschema:"shareable URL slug, lowercase, 3-64 chars, [a-z0-9-], e.g. dubrovnik-oct-2026"`
	Name                    string `json:"name" jsonschema:"display name, e.g. IRL Dubrovnik October 2026"`
	Country                 string `json:"country,omitempty"`
	City                    string `json:"city,omitempty"`
	HotelName               string `json:"hotelName,omitempty"`
	HotelAddress            string `json:"hotelAddress,omitempty"`
	HotelLink               string `json:"hotelLink,omitempty" jsonschema:"URL to the hotel website or booking page"`
	Timezone                string `json:"timezone,omitempty" jsonschema:"IANA timezone (e.g. Europe/Paris); defaults to the server's configured default"`
	StartDate               string `json:"startDate" jsonschema:"first travel day, YYYY-MM-DD (event-local)"`
	EndDate                 string `json:"endDate" jsonschema:"last travel day, YYYY-MM-DD (event-local)"`
	SubmissionDeadlineLocal string `json:"submissionDeadlineLocal" jsonschema:"deadline as event-local wall-clock, YYYY-MM-DDTHH:MM"`
	ReminderDaysBefore      *int   `json:"reminderDaysBefore,omitempty" jsonschema:"daily reminders this many days before the deadline (default 3)"`
	WeeklyReminders         *bool  `json:"weeklyReminders,omitempty" jsonschema:"send a weekly reminder to non-responders (default true)"`
	ReminderHour            *int   `json:"reminderHour,omitempty" jsonschema:"hour-of-day 0-23 in the event timezone for reminders (default 9)"`
	DailyActivityEmail      *bool  `json:"dailyActivityEmail,omitempty" jsonschema:"send a daily activity summary to the People team email (default false); admins set their own per-event notifications in the app"`
}

// mcpUpdateEventIn is a partial update: only the fields present are changed; the
// rest are carried over from the current event config. Day types are not editable
// over MCP (they stay in the admin UI), so the default first/last=travel layout
// is regenerated from the date range on every update.
type mcpUpdateEventIn struct {
	Event                   string  `json:"event" jsonschema:"the event slug or id to update"`
	Slug                    *string `json:"slug,omitempty"`
	Name                    *string `json:"name,omitempty"`
	Country                 *string `json:"country,omitempty"`
	City                    *string `json:"city,omitempty"`
	HotelName               *string `json:"hotelName,omitempty"`
	HotelAddress            *string `json:"hotelAddress,omitempty"`
	HotelLink               *string `json:"hotelLink,omitempty" jsonschema:"URL to the hotel website or booking page"`
	Timezone                *string `json:"timezone,omitempty" jsonschema:"IANA timezone (e.g. Europe/Paris)"`
	StartDate               *string `json:"startDate,omitempty" jsonschema:"YYYY-MM-DD"`
	EndDate                 *string `json:"endDate,omitempty" jsonschema:"YYYY-MM-DD"`
	SubmissionDeadlineLocal *string `json:"submissionDeadlineLocal,omitempty" jsonschema:"event-local wall-clock YYYY-MM-DDTHH:MM"`
	ReminderDaysBefore      *int    `json:"reminderDaysBefore,omitempty"`
	WeeklyReminders         *bool   `json:"weeklyReminders,omitempty"`
	ReminderHour            *int    `json:"reminderHour,omitempty"`
	DailyActivityEmail      *bool   `json:"dailyActivityEmail,omitempty"`
}

type mcpRosterRow struct {
	Name  string `json:"name" jsonschema:"full name"`
	Email string `json:"email" jsonschema:"work email"`
}

type mcpUploadRosterIn struct {
	Event string         `json:"event" jsonschema:"the event slug or id"`
	Rows  []mcpRosterRow `json:"rows" jsonschema:"name+email rows to add as attendees; additive — existing attendees are kept"`
}

type mcpAddAttendeeIn struct {
	Event string `json:"event" jsonschema:"the event slug or id"`
	Email string `json:"email" jsonschema:"the attendee's work email"`
	Name  string `json:"name,omitempty" jsonschema:"full name, used only when a new directory user must be provisioned for this email"`
}

type mcpRemoveAttendeeIn struct {
	Event string `json:"event" jsonschema:"the event slug or id"`
	Email string `json:"email" jsonschema:"the work email of the attendee to remove"`
}

// mcpSubmitResponseIn is the RSVP payload: the writable subset of a submission.
// It is always recorded for the calling user themselves. It mirrors the
// conditional form — fields outside the chosen branch are ignored/blanked
// server-side.
type mcpSubmitResponseIn struct {
	Event                string `json:"event,omitempty" jsonschema:"the event slug or id; omit to use the sole active event"`
	Attending            string `json:"attending" jsonschema:"yes, no, or not_sure"`
	NotSureReason        string `json:"notSureReason,omitempty" jsonschema:"required when attending=not_sure; ignored otherwise"`
	ArrivalDay           string `json:"arrivalDay,omitempty" jsonschema:"arrival date YYYY-MM-DD (event-local); required when attending=yes unless arrivalIndependent"`
	ArrivalTime          string `json:"arrivalTime,omitempty" jsonschema:"arrival time HH:MM; required when arrivalMode=flight, optional otherwise"`
	ArrivalMode          string `json:"arrivalMode,omitempty" jsonschema:"flight, car, train, or other; required when attending=yes unless arrivalIndependent"`
	ArrivalDetails       string `json:"arrivalDetails,omitempty" jsonschema:"flight number when arrivalMode=flight (required); free-text details otherwise (optional)"`
	DepartureDay         string `json:"departureDay,omitempty" jsonschema:"departure date YYYY-MM-DD (event-local); required when attending=yes unless departureIndependent"`
	DepartureTime        string `json:"departureTime,omitempty" jsonschema:"departure time HH:MM; required when departureMode=flight, optional otherwise"`
	DepartureMode        string `json:"departureMode,omitempty" jsonschema:"flight, car, train, or other; required when attending=yes unless departureIndependent"`
	DepartureDetails     string `json:"departureDetails,omitempty" jsonschema:"flight number when departureMode=flight (required); free-text details otherwise (optional)"`
	ArrivalIndependent   bool   `json:"arrivalIndependent,omitempty" jsonschema:"attendee self-arranges arrival; blanks the arrival leg"`
	DepartureIndependent bool   `json:"departureIndependent,omitempty" jsonschema:"attendee self-arranges departure; blanks the departure leg"`
	LongHaul             bool   `json:"longHaul,omitempty" jsonschema:"long-haul: needs accommodation / extra night before (only when at least one leg is People-team arranged)"`
	ExtraStayStart       string `json:"extraStayStart,omitempty" jsonschema:"company-paid extra night before the event (long-haul only), YYYY-MM-DD"`
	ExtraStaySelfFunded  bool   `json:"extraStaySelfFunded,omitempty" jsonschema:"attendee arrives the day before and arranges their own accommodation, but still wants company transport; mutually exclusive with extraStayStart"`
	Comments             string `json:"comments,omitempty" jsonschema:"free-text comments, optional"`
}

type mcpAttendee struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Attending   string `json:"attending"`   // yes | no | not_sure | no_response
	HasLoggedIn bool   `json:"hasLoggedIn"` // false = provisioned by import, never signed in
}

type mcpAttendeesOut struct {
	Event     string        `json:"event"`
	Attendees []mcpAttendee `json:"attendees"`
}

// --- helpers ---------------------------------------------------------------

func toolText(text string) []mcp.Content {
	return []mcp.Content{&mcp.TextContent{Text: text}}
}

// nilIfEmpty maps a trimmed-empty MCP string field to a nil *string, so an
// omitted optional (date/mode) round-trips as SQL NULL rather than "".
func nilIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// okResult renders both the human summary and the JSON payload into the text
// Content. The SDK also returns `out` as StructuredContent, but many MCP clients
// surface only text blocks; rendering the JSON beneath the summary keeps the data
// visible to the model in either case.
func okResult[T any](text string, out T) (*mcp.CallToolResult, T, error) {
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{Content: toolText(text)}, out, nil
	}
	return &mcp.CallToolResult{Content: toolText(text + "\n\n" + string(body))}, out, nil
}

// instrumentMCP wraps a tool handler so each call records a count + duration
// labelled by tool name and result (success|error).
func instrumentMCP[In any, Out any](
	name string,
	h func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error),
) func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		start := time.Now()
		res, out, err := h(ctx, req, in)
		metrics.MCPToolCallDuration.WithLabelValues(name).Observe(time.Since(start).Seconds())
		metrics.MCPToolCallsTotal.WithLabelValues(name, metrics.ResultLabel(err)).Inc()
		return res, out, err
	}
}

// resolveEventRef loads an event by slug (preferred) or id, normalising "not
// found" to a stable error string the model can act on.
func (a *App) resolveEventRef(ctx context.Context, ref string) (*Event, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, errors.New("event (slug or id) is required")
	}
	now := time.Now()
	e, err := a.Store.loadEventByColumn(ctx, "slug", strings.ToLower(ref), now)
	if err == nil {
		return e, nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if uuidRe.MatchString(ref) {
		e, err = a.Store.loadEventByColumn(ctx, "id", ref, now)
		if err == nil {
			return e, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	return nil, fmt.Errorf("event %q not found", ref)
}

// activeEventRefs returns the slugs of the active (non-past) events, soonest
// first — used to resolve the implicit event for the user tools and to list the
// choices when there's more than one.
func (a *App) activeEventRefs(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT slug, timezone, end_date FROM events ORDER BY start_date ASC`)
	if err != nil {
		return nil, fmt.Errorf("db error: %w", err)
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var slug, tz string
		var end time.Time
		if err := rows.Scan(&slug, &tz, &end); err != nil {
			return nil, fmt.Errorf("db error: %w", err)
		}
		loc, lerr := loadLocation(tz)
		if lerr != nil {
			loc = time.UTC
		}
		if isEventPast(end, loc, now) {
			continue
		}
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db error: %w", err)
	}
	return slugs, nil
}

// resolveUserEventRef picks the event a user tool acts on: the explicit ref when
// given, otherwise the sole active event. With no ref and zero-or-many active
// events it returns a guiding error rather than guessing.
func (a *App) resolveUserEventRef(ctx context.Context, ref string) (*Event, error) {
	if strings.TrimSpace(ref) != "" {
		return a.resolveEventRef(ctx, ref)
	}
	slugs, err := a.activeEventRefs(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	switch len(slugs) {
	case 0:
		return nil, errors.New("there is no active event right now")
	case 1:
		return a.resolveEventRef(ctx, slugs[0])
	default:
		return nil, fmt.Errorf("there are multiple active events — pass one as 'event': %s",
			strings.Join(slugs, ", "))
	}
}

// --- tool registration -----------------------------------------------------

func (a *App) registerMCPTools(s *mcp.Server) {
	// User tools — any signed-in employee.
	a.addToolGetProfile(s)
	a.addToolUpdateProfile(s)
	a.addToolListEvents(s)
	a.addToolGetEvent(s)
	a.addToolSubmitResponse(s)
	// Admin tools.
	a.addToolGetDashboard(s)
	a.addToolListNonResponders(s)
	a.addToolListSubmissions(s)
	a.addToolListAttendees(s)
	a.addToolGetActivity(s)
	a.addToolCreateEvent(s)
	a.addToolUpdateEvent(s)
	a.addToolUploadRoster(s)
	a.addToolAddAttendee(s)
	a.addToolRemoveAttendee(s)
	a.addToolTriggerReminders(s)
}

// --- read tools ------------------------------------------------------------

func (a *App) addToolListEvents(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_events",
		Title:       "List events",
		Description: "List events. Admins see every event (current and past) with response and roster counts; regular users see the active (non-past) events annotated with their own RSVP state.",
	}, instrumentMCP("list_events", func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmptyIn) (*mcp.CallToolResult, any, error) {
		user, err := requireMCPUser(ctx)
		if err != nil {
			return nil, nil, err
		}
		if !user.IsAdmin {
			return a.listEventsForUser(ctx, user)
		}
		rows, err := a.DB.QueryContext(ctx,
			`SELECT e.id, e.slug, e.name, e.country, e.city, e.timezone,
			        e.start_date, e.end_date, e.submission_deadline,
			        (SELECT count(*) FROM submissions s WHERE s.event_id = e.id),
			        (SELECT count(*) FROM event_attendees ea
			           JOIN users u ON u.id = ea.user_id
			          WHERE ea.event_id = e.id AND NOT u.archived)
			   FROM events e ORDER BY e.start_date DESC`)
		if err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		defer rows.Close()
		now := time.Now()
		out := mcpListEventsOut{Events: []mcpEventSummary{}}
		for rows.Next() {
			var e mcpEventSummary
			var start, end, deadline time.Time
			if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Country, &e.City, &e.Timezone,
				&start, &end, &deadline, &e.Responses, &e.RosterTotal); err != nil {
				return nil, nil, fmt.Errorf("db error: %w", err)
			}
			e.StartDate = start.Format(dateLayout)
			e.EndDate = end.Format(dateLayout)
			e.SubmissionDeadline = deadline.UTC()
			loc, lerr := loadLocation(e.Timezone)
			if lerr != nil {
				loc = time.UTC
			}
			e.IsPast = isEventPast(end, loc, now)
			out.Events = append(out.Events, e)
		}
		if err := rows.Err(); err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		res, val, err := okResult(fmt.Sprintf("%d event(s)", len(out.Events)), out)
		return res, val, err
	}))
}

// listEventsForUser is the non-admin branch of list_events: the active (non-past)
// events soonest-first, each annotated with the caller's own RSVP, and without
// the response/roster counts that are admin-only. Mirrors handleListCurrentEvents.
func (a *App) listEventsForUser(ctx context.Context, user *User) (*mcp.CallToolResult, any, error) {
	rows, err := a.DB.QueryContext(ctx,
		`SELECT e.id, e.slug, e.name, e.country, e.city, e.timezone,
		        e.start_date, e.end_date, e.submission_deadline, s.attending
		   FROM events e
		   LEFT JOIN submissions s ON s.event_id = e.id AND s.user_id = $1
		  ORDER BY e.start_date ASC`, user.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("db error: %w", err)
	}
	defer rows.Close()
	now := time.Now()
	out := mcpMyEventsOut{Events: []mcpMyEventSummary{}}
	for rows.Next() {
		var e mcpMyEventSummary
		var start, end, deadline time.Time
		var attending sql.NullString
		if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Country, &e.City, &e.Timezone,
			&start, &end, &deadline, &attending); err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		loc, lerr := loadLocation(e.Timezone)
		if lerr != nil {
			loc = time.UTC
		}
		if isEventPast(end, loc, now) {
			continue
		}
		e.StartDate = start.Format(dateLayout)
		e.EndDate = end.Format(dateLayout)
		e.SubmissionDeadline = deadline.UTC()
		if attending.Valid {
			e.HasSubmitted = true
			e.MyAttending = attending.String
		}
		out.Events = append(out.Events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("db error: %w", err)
	}
	res, val, err := okResult(fmt.Sprintf("%d active event(s)", len(out.Events)), out)
	return res, val, err
}

func (a *App) addToolGetEvent(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_event",
		Title:       "Get event",
		Description: "Read an event's full config: dates, typed days, hotel, timezone, deadline, and reminder settings. Any signed-in user may call it; omit 'event' to read the sole active event.",
	}, instrumentMCP("get_event", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEventRefOptIn) (*mcp.CallToolResult, *Event, error) {
		if _, err := requireMCPUser(ctx); err != nil {
			return nil, nil, err
		}
		e, err := a.resolveUserEventRef(ctx, in.Event)
		if err != nil {
			return nil, nil, err
		}
		return okResult(fmt.Sprintf("event %q (%s → %s)", e.Name, e.StartDate, e.EndDate), e)
	}))
}

// mcpProfileOut is the caller's own profile (the writable bits of users plus the
// derived display name and the confirm flag).
type mcpProfileOut struct {
	Email            string `json:"email"`
	FirstName        string `json:"firstName"`
	LastName         string `json:"lastName"`
	Name             string `json:"name"`
	Allergies        string `json:"allergies"`
	ProfileConfirmed bool   `json:"profileConfirmed"`
	IsAdmin          bool   `json:"isAdmin"`
}

type mcpUpdateProfileIn struct {
	FirstName string `json:"firstName" jsonschema:"your given name (required)"`
	LastName  string `json:"lastName" jsonschema:"your family name (required)"`
	Allergies string `json:"allergies,omitempty" jsonschema:"allergies / dietary preferences, free-form; pass empty to clear"`
}

func profileOut(u *User) mcpProfileOut {
	return mcpProfileOut{
		Email:            u.Email,
		FirstName:        u.FirstName,
		LastName:         u.LastName,
		Name:             strings.TrimSpace(u.FirstName + " " + u.LastName),
		Allergies:        u.Allergies,
		ProfileConfirmed: u.ProfileConfirmed,
		IsAdmin:          u.IsAdmin,
	}
}

func (a *App) addToolGetProfile(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_profile",
		Title:       "Get my profile",
		Description: "Read your own profile: name, allergies/dietary preferences, and whether you've confirmed it yet. Available to any signed-in user.",
	}, instrumentMCP("get_profile", func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmptyIn) (*mcp.CallToolResult, mcpProfileOut, error) {
		var zero mcpProfileOut
		u, err := requireMCPUser(ctx)
		if err != nil {
			return nil, zero, err
		}
		out := profileOut(u)
		summary := fmt.Sprintf("profile for %s", u.Email)
		if !out.ProfileConfirmed {
			summary += " (not yet confirmed — use update_profile)"
		}
		return okResult(summary, out)
	}))
}

func (a *App) addToolUpdateProfile(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_profile",
		Title:       "Update my profile",
		Description: "Update your own profile (name + allergies/dietary preferences) and mark it confirmed. This is the confirm step that submit_response requires. Available to any signed-in user.",
	}, instrumentMCP("update_profile", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUpdateProfileIn) (*mcp.CallToolResult, mcpProfileOut, error) {
		var zero mcpProfileOut
		u, err := requireMCPUser(ctx)
		if err != nil {
			return nil, zero, err
		}
		first := strings.TrimSpace(in.FirstName)
		last := strings.TrimSpace(in.LastName)
		allergies := strings.TrimSpace(in.Allergies)
		if first == "" || last == "" {
			return nil, zero, errors.New("first name and last name are required")
		}
		// Saving also marks the profile confirmed — mirrors handleUpdateMe.
		if _, err := a.DB.ExecContext(ctx,
			`UPDATE users SET first_name = $1, last_name = $2, allergies = $3, profile_confirmed = true WHERE id = $4`,
			first, last, allergies, u.ID); err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		u.FirstName, u.LastName, u.Allergies, u.ProfileConfirmed = first, last, allergies, true
		return okResult(fmt.Sprintf("profile confirmed for %s", u.Email), profileOut(u))
	}))
}

func (a *App) addToolGetDashboard(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_dashboard",
		Title:       "Get dashboard",
		Description: "Attending-state counts (yes/no/not_sure/no_response) and the per-attendee breakdown for an event. Every attendee is a company-directory user.",
	}, instrumentMCP("get_dashboard", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEventRefIn) (*mcp.CallToolResult, Dashboard, error) {
		var zero Dashboard
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		entries, counts, err := a.Store.dashboardEntries(ctx, e.ID)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		out := Dashboard{
			Total:   len(entries),
			Counts:  counts,
			Entries: entries,
		}
		return okResult(fmt.Sprintf("%s: %d yes / %d no / %d not sure / %d no response (of %d)",
			e.Name, counts["yes"], counts["no"], counts["notSure"], counts["noResponse"], out.Total), out)
	}))
}

func (a *App) addToolListNonResponders(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_non_responders",
		Title:       "List non-responders",
		Description: "Attendees with no submission yet, by name — a focused shortcut over get_dashboard.",
	}, instrumentMCP("list_non_responders", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEventRefIn) (*mcp.CallToolResult, mcpNonRespondersOut, error) {
		var zero mcpNonRespondersOut
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		entries, _, err := a.Store.dashboardEntries(ctx, e.ID)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		out := mcpNonRespondersOut{Event: e.Name, NonResponders: []mcpNonResponder{}}
		for _, en := range entries {
			if en.Attending == "no_response" {
				out.NonResponders = append(out.NonResponders, mcpNonResponder{FullName: en.Name, Email: en.Email})
			}
		}
		return okResult(fmt.Sprintf("%d non-responder(s) for %q", len(out.NonResponders), e.Name), out)
	}))
}

func (a *App) addToolListSubmissions(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_submissions",
		Title:       "List submissions",
		Description: "Submissions for an event, optionally filtered by attending state (mirrors the export filter).",
	}, instrumentMCP("list_submissions", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpListSubmissionsIn) (*mcp.CallToolResult, mcpSubmissionsOut, error) {
		var zero mcpSubmissionsOut
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		filter := parseAttendingFilter(in.Attending)

		rows, err := a.DB.QueryContext(ctx,
			`SELECT user_id FROM submissions WHERE event_id = $1`, e.ID)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		var userIDs []string
		for rows.Next() {
			var uid string
			if err := rows.Scan(&uid); err != nil {
				rows.Close()
				return nil, zero, fmt.Errorf("db error: %w", err)
			}
			userIDs = append(userIDs, uid)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}

		out := mcpSubmissionsOut{Event: e.Name, Submissions: []Submission{}}
		for _, uid := range userIDs {
			sub, err := a.Store.loadSubmission(ctx, e.ID, uid)
			if err != nil {
				return nil, zero, fmt.Errorf("db error: %w", err)
			}
			if len(filter) > 0 && !filter[sub.Attending] {
				continue
			}
			out.Submissions = append(out.Submissions, *sub)
		}
		return okResult(fmt.Sprintf("%d submission(s) for %q", len(out.Submissions), e.Name), out)
	}))
}

func (a *App) addToolListAttendees(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_attendees",
		Title:       "List attendees",
		Description: "List everyone expected at an event (the attendee roster) with their response state and whether they've ever signed in. Every attendee is a company-directory user.",
	}, instrumentMCP("list_attendees", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEventRefIn) (*mcp.CallToolResult, mcpAttendeesOut, error) {
		var zero mcpAttendeesOut
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		entries, _, err := a.Store.dashboardEntries(ctx, e.ID)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		out := mcpAttendeesOut{Event: e.Name, Attendees: []mcpAttendee{}}
		for _, en := range entries {
			out.Attendees = append(out.Attendees, mcpAttendee{
				Name:        en.Name,
				Email:       en.Email,
				Attending:   en.Attending,
				HasLoggedIn: en.HasLoggedIn,
			})
		}
		return okResult(fmt.Sprintf("%d attendee(s) for %q", len(out.Attendees), e.Name), out)
	}))
}

func (a *App) addToolGetActivity(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_activity",
		Title:       "Get activity",
		Description: "Recent activity-log entries for an event, newest first (after-deadline changes are flagged). Filter by category: 'user' for participant actions, 'admin' for administrative ones.",
	}, instrumentMCP("get_activity", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpGetActivityIn) (*mcp.CallToolResult, mcpActivityOut, error) {
		var zero mcpActivityOut
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		if in.Category != "" && in.Category != categoryUser && in.Category != categoryAdmin {
			return nil, zero, fmt.Errorf("category must be 'user' or 'admin'")
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		entries, err := a.queryActivity(ctx, e.ID, "", in.Category)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		return okResult(fmt.Sprintf("%d activity entr(ies) for %q", len(entries), e.Name),
			mcpActivityOut{Event: e.Name, Activity: entries})
	}))
}

// --- write tools -----------------------------------------------------------

func (a *App) addToolCreateEvent(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_event",
		Title:       "Create event",
		Description: "Create an event and generate its typed days (first/last = travel). Validates the slug and timezone.",
	}, instrumentMCP("create_event", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpCreateEventIn) (*mcp.CallToolResult, *Event, error) {
		user, err := requireMCPAdmin(ctx)
		if err != nil {
			return nil, nil, err
		}

		req := eventReq{
			Slug:                    in.Slug,
			Name:                    in.Name,
			Country:                 in.Country,
			City:                    in.City,
			HotelName:               in.HotelName,
			HotelAddress:            in.HotelAddress,
			HotelLink:               in.HotelLink,
			Timezone:                in.Timezone,
			StartDate:               in.StartDate,
			EndDate:                 in.EndDate,
			SubmissionDeadlineLocal: in.SubmissionDeadlineLocal,
			ReminderDaysBefore:      3,
			WeeklyReminders:         true,
			ReminderHour:            9,
		}
		if strings.TrimSpace(req.Timezone) == "" {
			req.Timezone = a.Cfg.DefaultEventTimezone
		}
		if in.ReminderDaysBefore != nil {
			req.ReminderDaysBefore = *in.ReminderDaysBefore
		}
		if in.WeeklyReminders != nil {
			req.WeeklyReminders = *in.WeeklyReminders
		}
		if in.ReminderHour != nil {
			req.ReminderHour = *in.ReminderHour
		}
		if in.DailyActivityEmail != nil {
			req.DailyActivityEmail = *in.DailyActivityEmail
		}

		start, end, deadlineUTC, days, verr := req.validateAndNormalize()
		if verr != nil {
			return nil, nil, verr
		}

		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		var id string
		err = tx.QueryRowContext(ctx,
			`INSERT INTO events (slug, name, country, city, hotel_name, hotel_address, hotel_link, timezone,
			        start_date, end_date, submission_deadline, reminder_days_before,
			        weekly_reminders, reminder_hour, daily_activity_email, created_by)
			 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
			req.Slug, req.Name, req.Country, req.City, req.HotelName, req.HotelAddress, req.HotelLink, req.Timezone,
			start, end, deadlineUTC, req.ReminderDaysBefore, req.WeeklyReminders, req.ReminderHour,
			req.DailyActivityEmail, user.ID).Scan(&id)
		if err != nil {
			metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
			if isUniqueViolation(err) {
				return nil, nil, errors.New("an event with that slug already exists")
			}
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		if err := insertDays(ctx, tx, id, days); err != nil {
			metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		// Everyone is an attendee by default — snapshot every existing user (mirrors
		// the REST handleCreateEvent path).
		if err := seedAllUsersAsAttendees(ctx, tx, id); err != nil {
			metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		summary := fmt.Sprintf("%s created event %q via MCP", user.Email, req.Name)
		if err := a.logActivity(ctx, tx, id, &user.ID, user.Email, "", actionEventCreated, summary, nil, false); err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		if err := tx.Commit(); err != nil {
			metrics.EventMutationsTotal.WithLabelValues("create", "error").Inc()
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		committed = true
		metrics.EventMutationsTotal.WithLabelValues("create", "success").Inc()

		e, err := a.Store.loadEventByColumn(ctx, "id", id, time.Now())
		if err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		return okResult(fmt.Sprintf("created event %q (slug %s)", e.Name, e.Slug), e)
	}))
}

func (a *App) addToolUpdateEvent(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_event",
		Title:       "Update event",
		Description: "Update an event's config and/or reminder settings. Only the fields you supply change; the rest are kept. Typed days are regenerated from the date range.",
	}, instrumentMCP("update_event", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUpdateEventIn) (*mcp.CallToolResult, *Event, error) {
		user, err := requireMCPAdmin(ctx)
		if err != nil {
			return nil, nil, err
		}
		cur, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, nil, err
		}

		// Seed the request from the current config, then overlay supplied fields.
		req := eventReq{
			Slug:                    cur.Slug,
			Name:                    cur.Name,
			Country:                 cur.Country,
			City:                    cur.City,
			HotelName:               cur.HotelName,
			HotelAddress:            cur.HotelAddress,
			HotelLink:               cur.HotelLink,
			Timezone:                cur.Timezone,
			StartDate:               cur.StartDate,
			EndDate:                 cur.EndDate,
			SubmissionDeadlineLocal: cur.SubmissionDeadlineLocal,
			ReminderDaysBefore:      cur.ReminderDaysBefore,
			WeeklyReminders:         cur.WeeklyReminders,
			ReminderHour:            cur.ReminderHour,
			DailyActivityEmail:      cur.DailyActivityEmail,
		}
		if in.Slug != nil {
			req.Slug = *in.Slug
		}
		if in.Name != nil {
			req.Name = *in.Name
		}
		if in.Country != nil {
			req.Country = *in.Country
		}
		if in.City != nil {
			req.City = *in.City
		}
		if in.HotelName != nil {
			req.HotelName = *in.HotelName
		}
		if in.HotelAddress != nil {
			req.HotelAddress = *in.HotelAddress
		}
		if in.HotelLink != nil {
			req.HotelLink = *in.HotelLink
		}
		if in.Timezone != nil {
			req.Timezone = *in.Timezone
		}
		if in.StartDate != nil {
			req.StartDate = *in.StartDate
		}
		if in.EndDate != nil {
			req.EndDate = *in.EndDate
		}
		if in.SubmissionDeadlineLocal != nil {
			req.SubmissionDeadlineLocal = *in.SubmissionDeadlineLocal
		}
		if in.ReminderDaysBefore != nil {
			req.ReminderDaysBefore = *in.ReminderDaysBefore
		}
		if in.WeeklyReminders != nil {
			req.WeeklyReminders = *in.WeeklyReminders
		}
		if in.ReminderHour != nil {
			req.ReminderHour = *in.ReminderHour
		}
		if in.DailyActivityEmail != nil {
			req.DailyActivityEmail = *in.DailyActivityEmail
		}

		start, end, deadlineUTC, days, verr := req.validateAndNormalize()
		if verr != nil {
			return nil, nil, verr
		}

		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		if _, err := tx.ExecContext(ctx,
			`UPDATE events SET slug=$1, name=$2, country=$3, city=$4, hotel_name=$5, hotel_address=$6,
			        hotel_link=$7, timezone=$8, start_date=$9, end_date=$10, submission_deadline=$11,
			        reminder_days_before=$12, weekly_reminders=$13, reminder_hour=$14,
			        daily_activity_email=$15, updated_at=now()
			   WHERE id=$16`,
			req.Slug, req.Name, req.Country, req.City, req.HotelName, req.HotelAddress, req.HotelLink, req.Timezone,
			start, end, deadlineUTC, req.ReminderDaysBefore, req.WeeklyReminders, req.ReminderHour,
			req.DailyActivityEmail, cur.ID); err != nil {
			metrics.EventMutationsTotal.WithLabelValues("update", "error").Inc()
			if isUniqueViolation(err) {
				return nil, nil, errors.New("an event with that slug already exists")
			}
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM event_days WHERE event_id = $1`, cur.ID); err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		if err := insertDays(ctx, tx, cur.ID, days); err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		summary := fmt.Sprintf("%s updated event %q via MCP", user.Email, req.Name)
		if err := a.logActivity(ctx, tx, cur.ID, &user.ID, user.Email, "", actionEventUpdated, summary, nil, false); err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		if err := tx.Commit(); err != nil {
			metrics.EventMutationsTotal.WithLabelValues("update", "error").Inc()
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		committed = true
		metrics.EventMutationsTotal.WithLabelValues("update", "success").Inc()

		e, err := a.Store.loadEventByColumn(ctx, "id", cur.ID, time.Now())
		if err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		return okResult(fmt.Sprintf("updated event %q (slug %s)", e.Name, e.Slug), e)
	}))
}

func (a *App) addToolUploadRoster(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "upload_roster",
		Title:       "Import attendees",
		Description: "Add employees to an event's attendee list from inline name+email rows. Provisions a company-directory user for each new email; additive (existing attendees are kept).",
	}, instrumentMCP("upload_roster", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpUploadRosterIn) (*mcp.CallToolResult, mcpStatusOut, error) {
		var zero mcpStatusOut
		user, err := requireMCPAdmin(ctx)
		if err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}

		// Validate + de-duplicate by lower-cased email, mirroring parseRosterCSV.
		seen := map[string]bool{}
		entries := make([]RosterEntry, 0, len(in.Rows))
		for _, row := range in.Rows {
			name := strings.TrimSpace(row.Name)
			email := strings.ToLower(strings.TrimSpace(row.Email))
			if name == "" || email == "" || !strings.Contains(email, "@") {
				continue
			}
			if seen[email] {
				continue
			}
			seen[email] = true
			entries = append(entries, RosterEntry{FullName: name, Email: email})
		}
		if len(entries) == 0 {
			return nil, zero, errors.New("no valid name,email rows provided")
		}

		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		added, err := provisionAttendees(ctx, tx, e.ID, entries)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		summary := fmt.Sprintf("%s imported %d attendee(s) via MCP", user.Email, added)
		if err := a.logActivity(ctx, tx, e.ID, &user.ID, user.Email, "", actionAttendeesImported, summary, nil, false); err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		committed = true

		return okResult(fmt.Sprintf("added %d attendee(s) to %q", added, e.Name),
			mcpStatusOut{OK: true, Message: fmt.Sprintf("%d attendees added", added)})
	}))
}

func (a *App) addToolAddAttendee(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_attendee",
		Title:       "Add attendee",
		Description: "Add a single person to an event's attendee list by email. If the email isn't a known directory user yet, a user is provisioned from it (optionally with a name) — that new employee is then a default attendee of every open event. Idempotent.",
	}, instrumentMCP("add_attendee", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAddAttendeeIn) (*mcp.CallToolResult, mcpStatusOut, error) {
		var zero mcpStatusOut
		user, err := requireMCPAdmin(ctx)
		if err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if email == "" || !strings.Contains(email, "@") {
			return nil, zero, errors.New("a valid email is required")
		}
		first, last := splitName(in.Name)

		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		userID, created, err := upsertDirectoryUser(ctx, tx, email, first, last)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		if created {
			if err := addUserToOpenEvents(ctx, tx, userID, time.Now()); err != nil {
				return nil, zero, fmt.Errorf("db error: %w", err)
			}
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO event_attendees (event_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			e.ID, userID)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		linked, _ := res.RowsAffected()
		if linked > 0 {
			summary := fmt.Sprintf("%s added %s as an attendee via MCP", user.Email, email)
			if err := a.logActivity(ctx, tx, e.ID, &user.ID, user.Email, email, actionAttendeeAdded, summary, nil, false); err != nil {
				return nil, zero, fmt.Errorf("db error: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		committed = true

		msg := fmt.Sprintf("%s is an attendee of %q", email, e.Name)
		switch {
		case created:
			msg = fmt.Sprintf("provisioned %s and added them to %q", email, e.Name)
		case linked == 0:
			msg = fmt.Sprintf("%s was already an attendee of %q", email, e.Name)
		}
		return okResult(msg, mcpStatusOut{OK: true, Message: msg})
	}))
}

func (a *App) addToolRemoveAttendee(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_attendee",
		Title:       "Remove attendee",
		Description: "Remove a person from an event's attendee list by email. Their directory record and any submission are left intact — only the event membership is removed. Idempotent.",
	}, instrumentMCP("remove_attendee", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpRemoveAttendeeIn) (*mcp.CallToolResult, mcpStatusOut, error) {
		var zero mcpStatusOut
		user, err := requireMCPAdmin(ctx)
		if err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if email == "" {
			return nil, zero, errors.New("email is required")
		}
		var userID string
		err = a.DB.QueryRowContext(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
		if err == sql.ErrNoRows {
			return nil, zero, fmt.Errorf("no user with email %q", email)
		}
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}

		tx, err := a.DB.BeginTx(ctx, nil)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback()
			}
		}()

		res, err := tx.ExecContext(ctx,
			`DELETE FROM event_attendees WHERE event_id = $1 AND user_id = $2`, e.ID, userID)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		removed, _ := res.RowsAffected()
		if removed > 0 {
			summary := fmt.Sprintf("%s removed %s from the attendee list via MCP", user.Email, email)
			if err := a.logActivity(ctx, tx, e.ID, &user.ID, user.Email, email, actionAttendeeRemoved, summary, nil, false); err != nil {
				return nil, zero, fmt.Errorf("db error: %w", err)
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		committed = true

		msg := fmt.Sprintf("removed %s from %q", email, e.Name)
		if removed == 0 {
			msg = fmt.Sprintf("%s was not an attendee of %q", email, e.Name)
		}
		return okResult(msg, mcpStatusOut{OK: true, Message: msg})
	}))
}

func (a *App) addToolSubmitResponse(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "submit_response",
		Title:       "Submit my RSVP / response",
		Description: "Record your own RSVP (attendance + travel) for an event — the same conditional-form write a participant makes in the app, with the server-side rules from DESIGN.md §8 enforced (fields outside the chosen branch are blanked). Always recorded for the calling user; omit 'event' to use the sole active event. You must confirm your profile first (use update_profile to set your name and allergies/dietary preferences). Upsert: re-running replaces your prior response and appends a revision + activity-log entry.",
	}, instrumentMCP("submit_response", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSubmitResponseIn) (*mcp.CallToolResult, *Submission, error) {
		user, err := requireMCPUser(ctx)
		if err != nil {
			return nil, nil, err
		}
		// Gate on profile confirmation: allergies/dietary preferences are joined
		// into the dashboard/export from the profile, so an RSVP is only useful once
		// the user has reviewed and confirmed it (mirrors the SPA's confirm step).
		if !user.ProfileConfirmed {
			return nil, nil, errors.New("confirm your profile first — set your name and allergies/dietary preferences with update_profile, then submit your RSVP")
		}
		e, err := a.resolveUserEventRef(ctx, in.Event)
		if err != nil {
			return nil, nil, err
		}
		// Users can't edit a past event (mirrors the employee HTTP path).
		if e.IsPast {
			return nil, nil, fmt.Errorf("event %q has ended and can no longer be edited", e.Name)
		}
		// Once an admin has edited this response it is locked: the attendee can no
		// longer change it (mirrors handlePutMySubmission).
		if existing, lerr := a.Store.loadSubmission(ctx, e.ID, user.ID); lerr == nil && existing.Locked {
			return nil, nil, errors.New("your response has been finalized by an organizer and can no longer be edited — contact the People team if something needs changing")
		} else if lerr != nil && lerr != sql.ErrNoRows {
			return nil, nil, fmt.Errorf("db error: %w", lerr)
		}

		req := submissionReq{
			Attending:            strings.TrimSpace(in.Attending),
			NotSureReason:        in.NotSureReason,
			ArrivalDay:           nilIfEmpty(in.ArrivalDay),
			ArrivalTime:          strings.TrimSpace(in.ArrivalTime),
			ArrivalMode:          nilIfEmpty(in.ArrivalMode),
			ArrivalDetails:       in.ArrivalDetails,
			DepartureDay:         nilIfEmpty(in.DepartureDay),
			DepartureTime:        strings.TrimSpace(in.DepartureTime),
			DepartureMode:        nilIfEmpty(in.DepartureMode),
			DepartureDetails:     in.DepartureDetails,
			ArrivalIndependent:   in.ArrivalIndependent,
			DepartureIndependent: in.DepartureIndependent,
			LongHaul:             in.LongHaul,
			ExtraStayStart:       nilIfEmpty(in.ExtraStayStart),
			ExtraStaySelfFunded:  in.ExtraStaySelfFunded,
			Comments:             in.Comments,
		}

		// Always the caller's own response: owner = actor = user, isAdmin=false
		// (full conditional-form validation applies, same as the SPA form), and
		// lock=false (only the interactive admin edit locks a response).
		sub, err := a.applySubmission(ctx, e, &req, user.ID, user, false, false)
		if err != nil {
			var inv errSubmissionInvalid
			if errors.As(err, &inv) {
				return nil, nil, err // validation message is already user-facing
			}
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		return okResult(
			fmt.Sprintf("recorded your response to %q: %s", e.Name, attendingLabel(sub.Attending)),
			sub)
	}))
}

func (a *App) addToolTriggerReminders(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "trigger_reminders",
		Title:       "Trigger reminders",
		Description: "Force the reminder + digest evaluation for an event now (idempotent via reminder_log). Sends only what is due in the event timezone and only if SMTP is configured.",
	}, instrumentMCP("trigger_reminders", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEventRefIn) (*mcp.CallToolResult, mcpStatusOut, error) {
		var zero mcpStatusOut
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, zero, err
		}
		if !a.Email.Configured() {
			return okResult("SMTP is not configured — nothing was sent",
				mcpStatusOut{OK: true, Message: "smtp not configured; no reminders sent"})
		}
		now := time.Now()
		a.processEventReminders(ctx, e, now)
		a.processEventDigest(ctx, e, now)
		return okResult(fmt.Sprintf("evaluated reminders + digest for %q", e.Name),
			mcpStatusOut{OK: true, Message: "reminder/digest evaluation run (idempotent per window)"})
	}))
}
