package forecast

import (
	"testing"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/risks"
)

var today = time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC) // a Thursday

func open(est *float64) risks.Issue { return risks.Issue{Estimated: est} }
func f(v float64) *float64          { return &v }
func d(s string) *time.Time         { t, _ := time.Parse(time.DateOnly, s); return &t }

func TestWorkingDays(t *testing.T) {
	c := DefaultCalendar()
	if got := c.AddWorkingDays(today, 2).Format(time.DateOnly); got != "2026-10-26" {
		t.Fatalf("Thu + 2 working days = %s, want Mon 26th", got)
	}
	c.Holidays = []string{"2026-10-23"}
	if got := c.AddWorkingDays(today, 1).Format(time.DateOnly); got != "2026-10-26" {
		t.Fatalf("holiday not skipped: %s", got)
	}
	if n := DefaultCalendar().WorkingDaysBetween(today, *d("2026-10-30")); n != 6 {
		t.Fatalf("between %d", n)
	}
}

func TestEffortForecastAgainstBaseline(t *testing.T) {
	// 4 estimated issues of 10 h, 40 h/week capacity -> 5 working days.
	issues := []risks.Issue{open(f(10)), open(f(10)), open(f(10)), open(f(10))}
	r := Compute(Input{Today: today, Issues: issues, WeeklyCapacity: 40, Baseline: d("2026-10-27"), CapacityUntilBaseline: -1, Calendar: DefaultCalendar()})
	if r.Method != MethodEffort || r.Forecast.Format(time.DateOnly) != "2026-10-29" {
		t.Fatalf("method %s forecast %v", r.Method, r.Forecast)
	}
	if r.Status != StatusAtRisk || len(r.Flags) != 1 || r.Flags[0].Kind != risks.KindForecastLate {
		t.Fatalf("status %s flags %+v", r.Status, r.Flags)
	}
	if r.Confidence != "high" {
		t.Fatalf("confidence %s", r.Confidence)
	}
	if *r.Baseline != *d("2026-10-27") {
		t.Fatal("baseline changed")
	}
}

func TestPartialEstimatesScaleAndLowerConfidence(t *testing.T) {
	issues := []risks.Issue{open(f(8)), open(nil)}
	r := Compute(Input{Today: today, Issues: issues, WeeklyCapacity: 40, CapacityUntilBaseline: -1, Calendar: DefaultCalendar()})
	if r.Method != MethodEffort || r.Inputs["scaled_remaining_hours"] != 16.0 || r.Confidence != "medium" {
		t.Fatalf("%+v", r)
	}
	if r.Status != StatusTBD {
		t.Fatal("no baseline must give status tbd")
	}
}

func TestNoDefensibleInputsIsTBD(t *testing.T) {
	issues := []risks.Issue{open(nil), open(nil), open(f(3))}
	r := Compute(Input{Today: today, Issues: issues, WeeklyCapacity: 40, Calendar: DefaultCalendar()})
	if r.Forecast != nil || r.Method != MethodNone || r.Confidence != "none" || r.Status != StatusTBD {
		t.Fatalf("%+v", r)
	}
}

func TestThroughputFallback(t *testing.T) {
	var issues []risks.Issue
	for w := 0; w < 4; w++ {
		c := today.AddDate(0, 0, -7*w-1)
		issues = append(issues, risks.Issue{IsClosed: true, ClosedOn: &c})
	}
	issues = append(issues, open(nil), open(nil))
	r := Compute(Input{Today: today, Issues: issues, Calendar: DefaultCalendar()})
	if r.Method != MethodThroughput || r.Forecast == nil || r.Confidence != "low" {
		t.Fatalf("%+v", r)
	}
	// 2 open at 0.5/week = 4 weeks = 20 working days.
	if r.Inputs["working_days_needed"] != 20 {
		t.Fatalf("days %v", r.Inputs["working_days_needed"])
	}
}

func TestBlockerAndCapacityRisk(t *testing.T) {
	issues := []risks.Issue{open(f(40))}
	r := Compute(Input{Today: today, Issues: issues, WeeklyCapacity: 40, BlockerDue: []time.Time{*d("2026-12-01")},
		Baseline: d("2026-12-31"), CapacityUntilBaseline: 20, Calendar: DefaultCalendar()})
	if r.Forecast.Format(time.DateOnly) != "2026-12-01" {
		t.Fatalf("forecast %v", r.Forecast)
	}
	kinds := map[string]bool{}
	for _, fl := range r.Flags {
		kinds[fl.Kind] = true
	}
	if !kinds[risks.KindCapacity] || kinds[risks.KindForecastLate] {
		t.Fatalf("flags %v", kinds)
	}
}

func TestDeliveredKeepsActualSeparate(t *testing.T) {
	r := Compute(Input{Today: today, Issues: []risks.Issue{open(nil)}, Baseline: d("2026-10-01"), Actual: d("2026-10-05"), Calendar: DefaultCalendar()})
	if r.Status != StatusDelivered || r.Actual.Format(time.DateOnly) != "2026-10-05" || r.Baseline.Format(time.DateOnly) != "2026-10-01" {
		t.Fatalf("%+v", r)
	}
}

func TestAwaitingQAIsNotRemainingEffort(t *testing.T) {
	qa := func(is risks.Issue) bool { return is.StatusName == "Fixed Not Tested" }
	fixed := risks.Issue{StatusName: "Fixed Not Tested", Estimated: f(30)}
	issues := []risks.Issue{open(f(10)), open(f(10)), fixed, fixed}
	r := Compute(Input{Today: today, Issues: issues, AwaitingQA: qa, WeeklyCapacity: 40, CapacityUntilBaseline: -1, Calendar: DefaultCalendar()})
	if r.Method != MethodEffort || r.Inputs["scaled_remaining_hours"] != 20.0 || r.Inputs["awaiting_qa_issues"] != 2 || r.Inputs["open_issues"] != 2 {
		t.Fatalf("%+v", r.Inputs)
	}
	only := Compute(Input{Today: today, Issues: []risks.Issue{fixed}, AwaitingQA: qa, WeeklyCapacity: 40, Calendar: DefaultCalendar()})
	if only.Status != StatusTBD || only.Forecast != nil || only.Actual != nil {
		t.Fatalf("only QA left must stay undelivered and TBD: %+v", only)
	}
}
