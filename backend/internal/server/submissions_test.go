package server

import "testing"

func sampleEvent() *Event {
	return &Event{StartDate: "2026-10-12", EndDate: "2026-10-16"}
}

func strp(s string) *string { return &s }

func TestSubmissionNotSureRequiresReason(t *testing.T) {
	req := &submissionReq{FirstName: "A", LastName: "B", Attending: "not_sure"}
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
		FirstName: "A", LastName: "B", Attending: "no",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"),
		Allergies: "nuts", Comments: "hi", NotSureReason: "x",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.ArrivalDay != nil || req.ArrivalMode != nil || req.Allergies != "" || req.Comments != "" || req.NotSureReason != "" {
		t.Errorf("No branch should blank travel/dietary/reason: %+v", req)
	}
}

func TestSubmissionYesRequiresTravel(t *testing.T) {
	req := &submissionReq{FirstName: "A", LastName: "B", Attending: "yes"}
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("expected error: yes without arrival")
	}
}

func TestSubmissionYesModeRequiresDetails(t *testing.T) {
	req := &submissionReq{
		FirstName: "A", LastName: "B", Attending: "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalDetails: "",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("flight"), DepartureDetails: "BA123",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err == nil {
		t.Fatal("expected error: arrival mode set but no details")
	}
}

func TestSubmissionValidYes(t *testing.T) {
	req := &submissionReq{
		FirstName: "A", LastName: "B", Attending: "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("train"), DepartureDetails: "TGV 9876",
	}
	if err := req.normalizeAndValidate(sampleEvent(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubmissionArrivalDayOutOfRange(t *testing.T) {
	req := &submissionReq{
		FirstName: "A", LastName: "B", Attending: "yes",
		ArrivalDay: strp("2026-09-01"), ArrivalMode: strp("flight"), ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("flight"), DepartureDetails: "BA200",
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
		FirstName: "A", LastName: "B", Attending: "yes",
		ArrivalDay: strp("2026-10-12"), ArrivalMode: strp("flight"), ArrivalDetails: "BA100",
		DepartureDay: strp("2026-10-16"), DepartureMode: strp("flight"), DepartureDetails: "BA200",
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
