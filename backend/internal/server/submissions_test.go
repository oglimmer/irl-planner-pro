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

// A flight requires both a time and a flight number, for employees and admins
// alike (it is a data-completeness rule, not a relaxable window).
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
	// Same rule for admins — no relaxation.
	if err := missingNumber.normalizeAndValidate(sampleEvent(), true); err == nil {
		t.Error("flight without a flight number should be rejected for admins too")
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

func TestExtraNightOneDayAllowedForEmployee(t *testing.T) {
	req := baseYes()
	req.ExtraStayStart = strp("2026-10-11") // one night before start (10-12)
	req.ExtraStayEnd = strp("2026-10-17")   // one night after end (10-16)
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("one extra night each side should be allowed: %v", err)
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
