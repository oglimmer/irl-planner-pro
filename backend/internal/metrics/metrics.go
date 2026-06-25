// Package metrics owns the Prometheus registry and exposes the counters,
// middleware, and /metrics handler used across the rest of the backend.
package metrics

import (
	"database/sql"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// registry is a process-local registry so /metrics output is deterministic
// regardless of which third-party libraries the binary happens to import.
var registry = prometheus.NewRegistry()

var (
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "irl_http_requests_total",
			Help: "Total HTTP requests by method, route pattern, and status code.",
		},
		[]string{"method", "route", "code"},
	)

	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "irl_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route"},
	)

	HTTPRequestsInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "irl_http_requests_in_flight",
			Help: "Number of HTTP requests currently being served.",
		},
	)

	LoginsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "irl_logins_total",
			Help: "Login attempts by mode and result (success|failure).",
		},
		[]string{"mode", "result"},
	)

	EventMutationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "irl_event_mutations_total",
			Help: "Event mutation count by action (create|update) and result.",
		},
		[]string{"action", "result"},
	)

	SubmissionMutationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "irl_submission_mutations_total",
			Help: "Submission mutation count by action (create|update|admin_edit) and result.",
		},
		[]string{"action", "result"},
	)

	RemindersSentTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "irl_reminders_sent_total",
			Help: "Reminder/digest emails sent by kind (weekly|deadline|daily_digest).",
		},
		[]string{"kind"},
	)

	MCPToolCallsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "irl_mcp_tool_calls_total",
			Help: "MCP tool invocations by tool name and result (success|error).",
		},
		[]string{"tool", "result"},
	)

	MCPToolCallDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "irl_mcp_tool_call_duration_seconds",
			Help:    "MCP tool invocation latency by tool name.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tool"},
	)
)

func init() {
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		HTTPRequestsTotal,
		HTTPRequestDuration,
		HTTPRequestsInFlight,
		LoginsTotal,
		EventMutationsTotal,
		SubmissionMutationsTotal,
		RemindersSentTotal,
		MCPToolCallsTotal,
		MCPToolCallDuration,
	)
}

// RegisterDBStats wires the *sql.DB pool stats into the registry. Called from
// main once the DB handle is available.
func RegisterDBStats(db *sql.DB) {
	registry.MustRegister(collectors.NewDBStatsCollector(db, "irlplanner"))
}

// Handler returns the /metrics http.Handler. When token is non-empty, the
// handler requires Authorization: Bearer <token>; otherwise it is open.
func Handler(token string) http.Handler {
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		Registry:      registry,
		ErrorHandling: promhttp.ContinueOnError,
	})
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status code written by the downstream handler so
// the metrics middleware can label by code.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it implements http.Flusher, so
// the MCP SSE stream isn't buffered.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware records request count, duration, and the in-flight gauge,
// labelled by chi route pattern so cardinality stays bounded. /metrics is
// excluded so scrape traffic doesn't contaminate the histogram.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		HTTPRequestsInFlight.Inc()
		defer HTTPRequestsInFlight.Dec()
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start).Seconds()

		route := "<unmatched>"
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		HTTPRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(elapsed)
	})
}

// ResultLabel maps an error to "success"/"error" for counter labels.
func ResultLabel(err error) string {
	if err == nil {
		return "success"
	}
	return "error"
}
