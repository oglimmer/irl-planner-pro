package server

import "testing"

func sampleEvent() *Event {
	return &Event{StartDate: "2026-10-12", EndDate: "2026-10-16"}
}

func strp(s string) *string { return &s }

func TestSubmissionNotSureRequiresReason(t *testing.T) {
	req := &submissionReq{Attending: "not_sure"}
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("expected error: not_sure without reason")
	}
	req.NotSureReason = "Waiting on visa"
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubmissionNoBlanksTravel(t *testing.T) {
	req := &submissionReq{
		Attending:  "no",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"),
		Comments: "hi", NotSureReason: "x",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ArrivalDay != nil || req.ArrivalMode != nil || req.Comments != "" || req.NotSureReason != "" {
		t.Errorf("No branch should blank travel/comments/reason: %+v", req)
	}
}

func TestSubmissionYesRequiresTravel(t *testing.T) {
	req := &submissionReq{Attending: "yes"}
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("expected error: yes without arrival")
	}
}

func TestSubmissionBothLegsIndependentSkipsAndBlanksEverything(t *testing.T) {
	// Both legs self-arranged: the legs, long-haul, and extra-night dates are all
	// blanked and leg validation is skipped (the old all-or-nothing case).
	req := &submissionReq{
		Attending:          "yes",
		ArrivalIndependent: true, DepartureIndependent: true,
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalDetails: "BA100",
		DepartureTime: "18:00", LongHaul: true, ExtraStayStart: strp("2026-10-11"),
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("fully independent travel should not require travel legs: %v", err)
	}
	if req.ArrivalDay != nil || req.ArrivalMode != nil || req.DepartureDay != nil ||
		req.ArrivalDetails != "" || req.DepartureTime != "" || req.LongHaul ||
		req.ExtraStayStart != nil || req.ExtraStayEnd != nil {
		t.Errorf("fully independent travel should blank all travel fields: %+v", req)
	}
	if !req.ArrivalIndependent || !req.DepartureIndependent {
		t.Error("both independent flags should stay true on a yes submission")
	}
}

// Independence is per-leg: an attendee can self-arrange arrival but still need
// the return booked. The independent leg is blanked; the other is still validated.
func TestSubmissionArrivalIndependentDepartureBooked(t *testing.T) {
	req := &submissionReq{
		Attending:          "yes",
		ArrivalIndependent: true,
		ArrivalDay:         strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("train"), DepartureDetails: "TGV 9876",
	}
	// Departure is by train, so its time/details stay optional even though set.
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("arrival-independent + booked departure should be valid: %v", err)
	}
	if req.ArrivalDay != nil || req.ArrivalMode != nil || req.ArrivalDetails != "" {
		t.Errorf("the independent arrival leg should be blanked: %+v", req)
	}
	if req.DepartureDay == nil || req.DepartureMode == nil || req.DepartureDetails == "" {
		t.Errorf("the booked departure leg should be preserved: %+v", req)
	}
}

// A leg flagged independent must not be rejected for missing day/mode/details.
func TestSubmissionDepartureIndependentNeedsNoDetails(t *testing.T) {
	req := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalTime: "09:00", ArrivalDetails: "BA100",
		DepartureIndependent: true,
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("departure-independent should not require departure details: %v", err)
	}
}

// Long-haul accommodation survives when only one leg is independent (the People
// team still handles the other), but is dropped only when both are independent.
func TestSubmissionLongHaulKeptWhenOneLegBooked(t *testing.T) {
	req := &submissionReq{
		Attending:          "yes",
		ArrivalIndependent: true,
		DepartureDay:       strp("2026-10-16"), DepartureMode: strp("flight"), DepartureTime: "18:00", DepartureDetails: "BA200",
		LongHaul: true, ExtraStayStart: strp("2026-10-11"),
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.LongHaul || req.ExtraStayStart == nil {
		t.Errorf("long-haul/extra night should be kept when a leg is still booked: %+v", req)
	}
}

func TestSubmissionIndependentTravelClearedWhenNotYes(t *testing.T) {
	req := &submissionReq{Attending: "no", ArrivalIndependent: true, DepartureIndependent: true}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ArrivalIndependent || req.DepartureIndependent {
		t.Error("independent flags should be cleared when not attending")
	}
}

// For non-flight modes, time and details (free text) stay optional: a leg with a
// day + mode but no time/details is valid.
func TestSubmissionNonFlightModeAllowsEmptyTimeAndDetails(t *testing.T) {
	req := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("car"), ArrivalDetails: "",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("train"), DepartureDetails: "",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("empty time/details should be allowed for non-flight modes: %v", err)
	}
}

// A flight requires both a time and a flight number for the employee form. Admins
// edit on the attendee's behalf with no validation (any option), so the same
// inputs are accepted on the admin path.
func TestSubmissionFlightRequiresTimeAndNumber(t *testing.T) {
	missingTime := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalTime: "", ArrivalDetails: "BA100",
		DepartureIndependent: true,
	}
	if err := missingTime.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Error("flight without a time should be rejected")
	}

	missingNumber := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalTime: "09:00", ArrivalDetails: "",
		DepartureIndependent: true,
	}
	if err := missingNumber.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Error("flight without a flight number should be rejected")
	}
	// Admins bypass field validation entirely.
	if err := missingNumber.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Errorf("admin flight without a flight number should be allowed: %v", err)
	}
}

// An admin edit drops every field-level requirement: no attendance reason, no
// required travel day/mode, any out-of-window dates — none of it is rejected.
func TestSubmissionAdminBypassesAllValidation(t *testing.T) {
	// "Not sure" with no reason, normally rejected for employees.
	noReason := &submissionReq{Attending: "not_sure"}
	if err := noReason.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Errorf("admin not_sure without a reason should be allowed: %v", err)
	}

	// "Yes" with no travel details at all, and a wildly out-of-window day.
	bare := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2025-01-01"), // years before the event
	}
	if err := bare.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Errorf("admin yes with missing/out-of-window travel should be allowed: %v", err)
	}
	// The out-of-window day is kept (canonicalized), not blanked or rejected.
	if bare.ArrivalDay == nil || *bare.ArrivalDay != "2025-01-01" {
		t.Errorf("admin arrival day should be preserved, got %v", bare.ArrivalDay)
	}
}

func TestSubmissionValidYes(t *testing.T) {
	req := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalTime: "09:00", ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("train"), DepartureDetails: "TGV 9876",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubmissionArrivalDayOutOfRange(t *testing.T) {
	req := &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-09-01"), ArrivalMode: strp("flight"), ArrivalTime: "09:00", ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("flight"), DepartureTime: "18:00", DepartureDetails: "BA200",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("expected error: arrival day far outside the event window")
	}
	// Admins are unrestricted on the date window.
	if err := req.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Fatalf("admin should be allowed an out-of-window date: %v", err)
	}
}

func baseYes() *submissionReq {
	return &submissionReq{
		Attending:  "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalTime: "09:00", ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("flight"), DepartureTime: "18:00", DepartureDetails: "BA200",
		LongHaul: true,
	}
}

func TestExtraNightBeforeOneDayAllowedForEmployee(t *testing.T) {
	req := baseYes()
	// The travel day must reflect the booked night: arrive the day before the
	// first day.
	req.ArrivalDay = strp("2026-10-11")
	req.ExtraStayStart = strp("2026-10-11") // one night before start (10-12)
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("one extra night before should be allowed: %v", err)
	}
}

// Reverse of the early-arrival rule: a booked extra night before with an in-window
// arrival is an orphan and must be removed (or the arrival day extended).
func TestExtraNightBeforeWithoutEarlyArrivalRejected(t *testing.T) {
	req := baseYes() // arrival 10-12 (the first day), long-haul
	req.ExtraStayStart = strp("2026-10-11")
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("an extra night before with an in-window arrival should be rejected")
	}
	// Admins keep the freedom to record it.
	req = baseYes()
	req.ExtraStayStart = strp("2026-10-11")
	if err := req.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Fatalf("admin should be allowed an unmatched extra night before: %v", err)
	}
}

func TestExtraNightTwoDaysRejectedForEmployeeAllowedForAdmin(t *testing.T) {
	req := baseYes()
	req.ExtraStayStart = strp("2026-10-10") // two nights before
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("employee should not be allowed two extra nights before")
	}
	req = baseYes()
	req.ExtraStayStart = strp("2026-10-10")
	if err := req.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Fatalf("admin should be allowed two extra nights: %v", err)
	}
}

func TestExtraStayClearedWhenNotLongHaul(t *testing.T) {
	req := baseYes()
	req.LongHaul = false
	req.ExtraStayStart = strp("2026-10-11")
	req.ExtraStayEnd = strp("2026-10-17")
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ExtraStayStart != nil || req.ExtraStayEnd != nil {
		t.Error("extra stay should be cleared when long_haul is false")
	}
}

// Arriving the day before the event (10-11, start is 10-12) is only valid when
// the long-haul extra night before is booked; otherwise it's rejected.
func TestEarlyArrivalRequiresExtraNightBefore(t *testing.T) {
	req := baseYes()
	req.ArrivalDay = strp("2026-10-11") // day before the first day
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("early arrival without the extra night before should be rejected")
	}

	// Not a long-haul traveller: ExtraStay* is blanked by the long-haul block, so
	// the early arrival is still rejected.
	req = baseYes()
	req.ArrivalDay = strp("2026-10-11")
	req.LongHaul = false
	req.ExtraStayStart = strp("2026-10-11")
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("early arrival without long-haul should be rejected")
	}

	// Long-haul with the matching extra night booked: accepted.
	req = baseYes()
	req.ArrivalDay = strp("2026-10-11")
	req.ExtraStayStart = strp("2026-10-11")
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("early arrival with the extra night before should be allowed: %v", err)
	}
}

// Late return is no longer offered: leaving the day after the event (10-17, end is
// 10-16) is out of the employee window and rejected even with an after-night set.
func TestLateDepartureRejected(t *testing.T) {
	req := baseYes()
	req.DepartureDay = strp("2026-10-17") // day after the last day
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("a departure the day after the event should be rejected")
	}

	// Even with an after-night supplied, the day-after departure stays out of window.
	req = baseYes()
	req.DepartureDay = strp("2026-10-17")
	req.ExtraStayEnd = strp("2026-10-17")
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("a day-after departure is out of window regardless of any after-night")
	}
}

// The after-night is always blanked — for employees and admins alike — since the
// company no longer provides a late return.
func TestExtraStayEndAlwaysBlanked(t *testing.T) {
	for _, isAdmin := range []bool{false, true} {
		req := baseYes()
		req.ExtraStayEnd = strp("2026-10-17")
		if err := req.normalizeAndValidate(sampleEvent(), isAdmin); err != nil {
			t.Fatalf("isAdmin=%v: unexpected error: %v", isAdmin, err)
		}
		if req.ExtraStayEnd != nil {
			t.Errorf("isAdmin=%v: extra_stay_end should always be blanked, got %v", isAdmin, *req.ExtraStayEnd)
		}
	}
}

// Like the date-window check, the consistency rule relaxes for admins: they may
// record an out-of-window day without the matching extra night for special cases.
func TestEarlyArrivalRuleRelaxedForAdmins(t *testing.T) {
	req := baseYes()
	req.ArrivalDay = strp("2026-10-11")
	if err := req.normalizeAndValidate(sampleEvent(), true); err != nil {
		t.Fatalf("admin early arrival without the extra night should be allowed: %v", err)
	}
}

// An attendee may arrive the day before and self-fund that night (no company hotel,
// not long-haul) while still wanting company transport — the self-funded flag
// legitimises the early arrival.
func TestEarlyArrivalSelfFundedAllowed(t *testing.T) {
	req := baseYes()
	req.LongHaul = false
	req.ArrivalDay = strp("2026-10-11") // day before the first day
	req.ExtraStaySelfFunded = true
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("self-funded early arrival should be allowed: %v", err)
	}
	if !req.ExtraStaySelfFunded {
		t.Error("self-funded flag should be preserved for a day-before arrival")
	}
	if req.ExtraStayStart != nil {
		t.Error("self-funded early arrival should not book a company night")
	}
}

// The company-paid night wins when both are somehow set; the self-funded flag is
// cleared so the two never coexist.
func TestSelfFundedClearedWhenCompanyNightBooked(t *testing.T) {
	req := baseYes() // longHaul true
	req.ArrivalDay = strp("2026-10-11")
	req.ExtraStayStart = strp("2026-10-11")
	req.ExtraStaySelfFunded = true
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ExtraStaySelfFunded {
		t.Error("self-funded flag should be cleared when the company night is booked")
	}
	if req.ExtraStayStart == nil {
		t.Error("the company night should be kept")
	}
}

// A self-funded flag with an in-window arrival is just a stray note: cleared, not
// an error (unlike an orphan company night).
func TestSelfFundedClearedWhenNotEarly(t *testing.T) {
	req := baseYes() // arrival 10-12 (in window)
	req.ExtraStaySelfFunded = true
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("a stray self-funded flag should not error: %v", err)
	}
	if req.ExtraStaySelfFunded {
		t.Error("self-funded flag should be cleared when not arriving the day before")
	}
}

// A self-arranged arrival leg has no company transport, so the self-funded flag is
// cleared even if supplied.
func TestSelfFundedClearedWhenArrivalIndependent(t *testing.T) {
	req := baseYes()
	req.ArrivalIndependent = true
	req.ExtraStaySelfFunded = true
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ExtraStaySelfFunded {
		t.Error("self-funded flag should be cleared for a self-arranged arrival")
	}
}

func TestDiffSubmissionReqReportsChangedFields(t *testing.T) {
	prev := submissionReq{
		Attending:   "yes",
		ArrivalDay:  strp("2026-10-12"),
		ArrivalTime: "09:00",
		ArrivalMode: strp("flight"),
		Comments:    "old",
	}
	next := submissionReq{
		Attending:   "yes",
		ArrivalDay:  strp("2026-10-13"),
		ArrivalTime: "09:00",
		ArrivalMode: strp("train"),
		Comments:    "old",
	}
	changes := diffSubmissionReq(prev, next)
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}
	if changes[0].Field != "Arrival day" || changes[0].From != "2026-10-12" || changes[0].To != "2026-10-13" {
		t.Errorf("unexpected arrival-day change: %+v", changes[0])
	}
	if changes[1].Field != "Arrival mode" || changes[1].From != "flight" || changes[1].To != "train" {
		t.Errorf("unexpected arrival-mode change: %+v", changes[1])
	}
}

func TestDiffSubmissionReqNoChanges(t *testing.T) {
	req := submissionReq{Attending: "yes", ArrivalDay: strp("2026-10-12")}
	if changes := diffSubmissionReq(req, req); len(changes) != 0 {
		t.Fatalf("expected no changes, got %+v", changes)
	}
}

func TestDiffSubmissionReqClearedFieldShowsEmptyTo(t *testing.T) {
	prev := submissionReq{Attending: "yes", ArrivalDetails: "Terminal 2"}
	next := submissionReq{Attending: "no"}
	changes := diffSubmissionReq(prev, next)
	// Attending yes→no and the cleared arrival details.
	var sawCleared bool
	for _, c := range changes {
		if c.Field == "Arrival details" {
			sawCleared = true
			if c.From != "Terminal 2" || c.To != "" {
				t.Errorf("expected details cleared to empty, got %+v", c)
			}
		}
	}
	if !sawCleared {
		t.Error("expected a cleared Arrival details change")
	}
}
