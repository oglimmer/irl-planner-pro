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

// Phase 7 MCP server (DESIGN.md §18). An additive, OAuth-gated, admin-scoped
// surface that lets an MCP client (e.g. Claude) query and manage events. It is a
// thin protocol adapter over the existing query/command functions in events.go,
// dashboard.go, roster.go, activity.go, and reminders.go — not a second copy of
// the business logic. Off unless MCP_OAUTH_CLIENT_* are configured.

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

// requireMCPAdmin enforces the same authorization as the REST admin group: the
// caller must be authenticated and an admin. Every tool — read and write —
// requires admin, so nothing is exposed via MCP that a non-admin couldn't reach
// through the SPA (DESIGN.md §18.1).
func requireMCPAdmin(ctx context.Context) (*User, error) {
	u := userFromCtx(ctx)
	if u == nil {
		return nil, errors.New("unauthenticated")
	}
	if !u.IsAdmin {
		return nil, errors.New("admin only")
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
	DailyActivityEmail      *bool  `json:"dailyActivityEmail,omitempty" jsonschema:"email admins a daily activity digest (default false)"`
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

// mcpSubmitResponseIn is the RSVP payload: the writable subset of a submission
// plus the attendee email it is recorded for. It mirrors the conditional form —
// fields outside the chosen branch are ignored/blanked server-side.
type mcpSubmitResponseIn struct {
	Event                string `json:"event" jsonschema:"the event slug or id"`
	Email                string `json:"email" jsonschema:"the attendee's work email; must be an existing directory user (use add_attendee first otherwise)"`
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
	LongHaul             bool   `json:"longHaul,omitempty" jsonschema:"long-haul: needs accommodation / extra nights (only when at least one leg is People-team arranged)"`
	ExtraStayStart       string `json:"extraStayStart,omitempty" jsonschema:"extra night before the event, YYYY-MM-DD"`
	ExtraStayEnd         string `json:"extraStayEnd,omitempty" jsonschema:"extra night after the event, YYYY-MM-DD"`
	Comments             string `json:"comments,omitempty" jsonschema:"free-text comments, optional"`
	AsAdmin              bool   `json:"asAdmin,omitempty" jsonschema:"record as an admin edit (relaxes the date-window and extra-night limits, and allows editing a past event) instead of a normal attendee RSVP; default false"`
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

// --- tool registration -----------------------------------------------------

func (a *App) registerMCPTools(s *mcp.Server) {
	a.addToolListEvents(s)
	a.addToolGetEvent(s)
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
	a.addToolSubmitResponse(s)
	a.addToolTriggerReminders(s)
}

// --- read tools ------------------------------------------------------------

func (a *App) addToolListEvents(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_events",
		Title:       "List events",
		Description: "List all events (current and past) with response and roster counts.",
	}, instrumentMCP("list_events", func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpEmptyIn) (*mcp.CallToolResult, mcpListEventsOut, error) {
		var zero mcpListEventsOut
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, zero, err
		}
		rows, err := a.DB.QueryContext(ctx,
			`SELECT e.id, e.slug, e.name, e.country, e.city, e.timezone,
			        e.start_date, e.end_date, e.submission_deadline,
			        (SELECT count(*) FROM submissions s WHERE s.event_id = e.id),
			        (SELECT count(*) FROM event_attendees ea WHERE ea.event_id = e.id)
			   FROM events e ORDER BY e.start_date DESC`)
		if err != nil {
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		defer rows.Close()
		now := time.Now()
		out := mcpListEventsOut{Events: []mcpEventSummary{}}
		for rows.Next() {
			var e mcpEventSummary
			var start, end, deadline time.Time
			if err := rows.Scan(&e.ID, &e.Slug, &e.Name, &e.Country, &e.City, &e.Timezone,
				&start, &end, &deadline, &e.Responses, &e.RosterTotal); err != nil {
				return nil, zero, fmt.Errorf("db error: %w", err)
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
			return nil, zero, fmt.Errorf("db error: %w", err)
		}
		return okResult(fmt.Sprintf("%d event(s)", len(out.Events)), out)
	}))
}

func (a *App) addToolGetEvent(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_event",
		Title:       "Get event",
		Description: "Read an event's full config: dates, typed days, hotel, timezone, deadline, and reminder settings.",
	}, instrumentMCP("get_event", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpEventRefIn) (*mcp.CallToolResult, *Event, error) {
		if _, err := requireMCPAdmin(ctx); err != nil {
			return nil, nil, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, nil, err
		}
		return okResult(fmt.Sprintf("event %q (%s → %s)", e.Name, e.StartDate, e.EndDate), e)
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
		Title:       "Submit RSVP / response",
		Description: "Record an attendee's RSVP (attendance + travel) for an event on their behalf — the same conditional-form write a participant makes in the app, with the server-side rules from DESIGN.md §8 enforced (fields outside the chosen branch are blanked). The email must be an existing directory user; use add_attendee first otherwise. Recorded as the attendee's own response by default; set asAdmin=true to record an admin edit that relaxes the date-window / extra-night limits and allows a past event. Upsert: re-running replaces the prior response and appends a revision + activity-log entry.",
	}, instrumentMCP("submit_response", func(ctx context.Context, _ *mcp.CallToolRequest, in mcpSubmitResponseIn) (*mcp.CallToolResult, *Submission, error) {
		admin, err := requireMCPAdmin(ctx)
		if err != nil {
			return nil, nil, err
		}
		e, err := a.resolveEventRef(ctx, in.Event)
		if err != nil {
			return nil, nil, err
		}
		// A normal attendee RSVP can't touch a past event (mirrors the employee
		// HTTP path); an admin edit can (mirrors the admin HTTP path).
		if e.IsPast && !in.AsAdmin {
			return nil, nil, fmt.Errorf("event %q has ended and can no longer be edited (use asAdmin to override)", e.Name)
		}
		email := strings.ToLower(strings.TrimSpace(in.Email))
		if email == "" || !strings.Contains(email, "@") {
			return nil, nil, errors.New("a valid email is required")
		}

		var owner User
		err = a.DB.QueryRowContext(ctx,
			`SELECT id, email, is_admin FROM users WHERE email = $1`, email).
			Scan(&owner.ID, &owner.Email, &owner.IsAdmin)
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("no user with email %q — add them with add_attendee first", email)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("db error: %w", err)
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
			ExtraStayEnd:         nilIfEmpty(in.ExtraStayEnd),
			Comments:             in.Comments,
		}

		// Default: record as the attendee's own response (actor = owner, not an
		// admin edit) — exactly what would land if they submitted the form
		// themselves. asAdmin attributes the change to the calling admin and
		// relaxes validation for special cases.
		actor := &owner
		if in.AsAdmin {
			actor = admin
		}

		sub, err := a.applySubmission(ctx, e, &req, owner.ID, actor, in.AsAdmin)
		if err != nil {
			var inv errSubmissionInvalid
			if errors.As(err, &inv) {
				return nil, nil, err // validation message is already user-facing
			}
			return nil, nil, fmt.Errorf("db error: %w", err)
		}
		return okResult(
			fmt.Sprintf("recorded %s's response to %q: %s", email, e.Name, attendingLabel(sub.Attending)),
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
