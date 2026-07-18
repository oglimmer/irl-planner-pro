package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// financialTargets are the report currencies every travel cost is converted to.
var financialTargets = []string{"USD", "GBP", "EUR"}

// FinancialRow is one attendee's declared travel cost in its original currency
// plus its value converted into each of financialTargets. Converted is nil when
// live FX rates are unavailable (or the row's currency has no rate).
type FinancialRow struct {
	UserID    string             `json:"userId"`
	Name      string             `json:"name"`
	Email     string             `json:"email"`
	Amount    float64            `json:"amount"`
	Currency  string             `json:"currency"`
	Converted map[string]float64 `json:"converted"`
}

// FinancialReport is the admin Financial tab payload: every attendee who
// declared a travel cost, with per-target conversions and grand totals. When
// RatesAvailable is false the FX API could not be reached; rows still list their
// original amounts (Converted nil) so the raw costs are never hidden.
type FinancialReport struct {
	Targets        []string           `json:"targets"`
	Rows           []FinancialRow     `json:"rows"`
	Totals         map[string]float64 `json:"totals"`
	RatesAvailable bool               `json:"ratesAvailable"`
	RatesAsOf      string             `json:"ratesAsOf"`
}

// handleFinancial returns the travel-cost report for an event: each attendee's
// declared cost converted to USD/GBP/EUR via live Frankfurter FX rates, plus
// totals. Costs are only stored on attending=yes responses, so a simple
// travel_cost NOT NULL filter yields the payers.
func (a *App) handleFinancial(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var exists bool
	if err := a.DB.QueryRowContext(r.Context(),
		`SELECT EXISTS (SELECT 1 FROM events WHERE id = $1)`, id).Scan(&exists); err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	if !exists {
		writeErr(w, http.StatusNotFound, "event not found")
		return
	}

	rows, err := a.DB.QueryContext(r.Context(),
		`SELECT u.id, u.first_name, u.last_name, u.email, s.travel_cost, s.travel_cost_currency
		   FROM event_attendees ea
		   JOIN users u ON u.id = ea.user_id
		   JOIN submissions s ON s.event_id = ea.event_id AND s.user_id = ea.user_id
		  WHERE ea.event_id = $1 AND NOT u.archived AND s.travel_cost IS NOT NULL
		  ORDER BY u.first_name, u.last_name, u.email`, id)
	if err != nil {
		serverErr(w, r, err, "db error")
		return
	}
	defer rows.Close()

	type payer struct {
		userID, name, email, currency string
		amount                        float64
	}
	var payers []payer
	for rows.Next() {
		var uid, first, last, email string
		var amount sql.NullFloat64
		var currency sql.NullString
		if err := rows.Scan(&uid, &first, &last, &email, &amount, &currency); err != nil {
			serverErr(w, r, err, "db error")
			return
		}
		name := strings.TrimSpace(first + " " + last)
		if name == "" {
			name = email
		}
		payers = append(payers, payer{uid, name, email, strings.ToUpper(currency.String), amount.Float64})
	}
	if err := rows.Err(); err != nil {
		serverErr(w, r, err, "db error")
		return
	}

	rates, asOf, ratesErr := fxRates.get(r.Context(), a.Cfg.FrankfurterBaseURL)
	ratesAvailable := ratesErr == nil
	if ratesErr != nil {
		// Non-fatal: the report still renders the original amounts.
		log.Printf("WARN: Frankfurter FX rates unavailable: %v", ratesErr)
	}

	report := FinancialReport{
		Targets:        financialTargets,
		Rows:           []FinancialRow{},
		Totals:         map[string]float64{},
		RatesAvailable: ratesAvailable,
		RatesAsOf:      asOf,
	}
	for _, p := range payers {
		row := FinancialRow{UserID: p.userID, Name: p.name, Email: p.email, Amount: p.amount, Currency: p.currency}
		if ratesAvailable {
			row.Converted = map[string]float64{}
			for _, t := range financialTargets {
				if v, ok := convertAmount(p.amount, p.currency, t, rates); ok {
					row.Converted[t] = round2(v)
					report.Totals[t] += v
				}
			}
		}
		report.Rows = append(report.Rows, row)
	}
	for t, v := range report.Totals {
		report.Totals[t] = round2(v)
	}

	writeJSON(w, http.StatusOK, report)
}

// --- currency conversion ---------------------------------------------------

// convertAmount converts amount from currency `from` to `to` using EUR-based
// rates (as returned by Frankfurter with base=EUR: rate[X] = units of X per 1
// EUR). It returns false when either currency has no rate. EUR itself is implicit
// (rate 1) since Frankfurter omits the base from its rates map.
func convertAmount(amount float64, from, to string, eurRates map[string]float64) (float64, bool) {
	rf, ok1 := currencyRate(from, eurRates)
	rt, ok2 := currencyRate(to, eurRates)
	if !ok1 || !ok2 || rf == 0 {
		return 0, false
	}
	return amount / rf * rt, true
}

func currencyRate(cur string, eurRates map[string]float64) (float64, bool) {
	if cur == "EUR" {
		return 1, true
	}
	v, ok := eurRates[cur]
	return v, ok
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// --- Frankfurter rate cache ------------------------------------------------

// frankfurterCache holds the latest EUR-based rate table, refreshed at most once
// per ttl. A stale table is served if a refresh fails, so a transient FX outage
// never blanks the report once rates have been seen.
type frankfurterCache struct {
	mu        sync.Mutex
	rates     map[string]float64
	asOf      string
	fetchedAt time.Time
	ttl       time.Duration
}

// fxRates is the process-wide rate cache. Rates move at most daily, so a 1h TTL
// keeps the Financial tab responsive without hammering the upstream API.
var fxRates = &frankfurterCache{ttl: time.Hour}

var frankfurterClient = &http.Client{Timeout: 8 * time.Second}

// get returns the cached EUR-based rates (with their as-of date), fetching afresh
// when the cache is empty or older than ttl. On a failed refresh it falls back to
// any previously cached table rather than failing outright.
func (c *frankfurterCache) get(ctx context.Context, baseURL string) (map[string]float64, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rates != nil && time.Since(c.fetchedAt) < c.ttl {
		return c.rates, c.asOf, nil
	}

	rates, asOf, err := fetchFrankfurterRates(ctx, baseURL)
	if err != nil {
		if c.rates != nil {
			return c.rates, c.asOf, nil // serve stale on failure
		}
		return nil, "", err
	}
	c.rates, c.asOf, c.fetchedAt = rates, asOf, time.Now()
	return c.rates, c.asOf, nil
}

// fetchFrankfurterRates pulls the current EUR-based rate table from the
// Frankfurter API (GET <base>/latest?base=EUR). The response omits EUR from its
// rates map (it is the base, rate 1), which convertAmount accounts for.
func fetchFrankfurterRates(ctx context.Context, baseURL string) (map[string]float64, string, error) {
	url := baseURL + "/latest?base=EUR"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := frankfurterClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("frankfurter: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, "", err
	}
	if len(body.Rates) == 0 {
		return nil, "", fmt.Errorf("frankfurter: empty rate table")
	}
	return body.Rates, body.Date, nil
}
