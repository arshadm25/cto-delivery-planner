// Package forecast implements the delivery forecasting rules from plan
// section 11. Forecasts are deliberately simple and explainable: every
// result lists its method, inputs, assumptions and confidence, and when no
// defensible inputs exist the forecast is TBD rather than a guess.
//
// Baseline, forecast and actual are kept separate. Nothing here changes a
// baseline.
package forecast

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/arshadm25/cto-delivery-planner/internal/risks"
)

// Calendar is the working calendar (Q7). Defaults are assumptions until the
// calendar is confirmed.
type Calendar struct {
	// WorkingDays are weekdays counted as working, Sunday = 0.
	WorkingDays []time.Weekday `json:"working_days"`
	HoursPerDay float64        `json:"hours_per_day"`
	Holidays    []string       `json:"holidays"`
	// Confirmed is false until someone records the real calendar.
	Confirmed bool `json:"confirmed"`
}

// DefaultCalendar is Monday to Friday, 8 hours, no holidays, unconfirmed.
func DefaultCalendar() Calendar {
	return Calendar{WorkingDays: []time.Weekday{1, 2, 3, 4, 5}, HoursPerDay: 8}
}

func (c Calendar) isWorking(d time.Time) bool {
	working := false
	for _, w := range c.WorkingDays {
		if d.Weekday() == w {
			working = true
		}
	}
	if !working {
		return false
	}
	s := d.Format(time.DateOnly)
	for _, h := range c.Holidays {
		if h == s {
			return false
		}
	}
	return true
}

// AddWorkingDays returns the date n working days after from (n >= 0).
func (c Calendar) AddWorkingDays(from time.Time, n int) time.Time {
	d := from
	if len(c.WorkingDays) == 0 {
		return d.AddDate(0, 0, n)
	}
	for n > 0 {
		d = d.AddDate(0, 0, 1)
		if c.isWorking(d) {
			n--
		}
	}
	return d
}

// WorkingDaysBetween counts working days after from up to and including to.
func (c Calendar) WorkingDaysBetween(from, to time.Time) int {
	n := 0
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if c.isWorking(d) {
			n++
		}
	}
	return n
}

// Input is everything one forecast needs.
type Input struct {
	Today time.Time
	// Issues in the forecast's scope (a project or one version), open and closed.
	Issues []risks.Issue
	// BlockerDue lists due dates of open issues outside the scope that block
	// issues inside it.
	BlockerDue []time.Time
	// WeeklyCapacity is approved allocated hours per week for this scope
	// over the coming period, or 0 when none is recorded.
	WeeklyCapacity float64
	// CapacityUntilBaseline is approved allocated hours between today and
	// the baseline, used for the capacity check. Negative means unknown.
	CapacityUntilBaseline float64
	Baseline              *time.Time
	Actual                *time.Time
	Calendar              Calendar
}

// Result is an explained forecast.
type Result struct {
	Baseline    *time.Time     `json:"baseline"`
	Forecast    *time.Time     `json:"forecast"`
	Actual      *time.Time     `json:"actual"`
	Method      string         `json:"method"`
	Confidence  string         `json:"confidence"`
	Status      string         `json:"status"`
	Inputs      map[string]any `json:"inputs"`
	Assumptions []string       `json:"assumptions"`
	Flags       []risks.Flag   `json:"-"`
}

// Methods and statuses.
const (
	MethodEffort     = "remaining_effort_over_capacity"
	MethodThroughput = "recent_throughput"
	MethodNone       = "tbd"

	StatusOnTrack   = "on_track"
	StatusAtRisk    = "at_risk"
	StatusTBD       = "tbd"
	StatusDelivered = "delivered"
)

// Minimum evidence for each method.
const (
	minCoverageEffort  = 0.5
	historyWeeks       = 8
	minClosedForTrend  = 3
	minWeeksWithCloses = 3
)

// Compute runs the rules.
func Compute(in Input) Result {
	cal := in.Calendar
	if cal.HoursPerDay <= 0 {
		cal.HoursPerDay = 8
	}
	res := Result{Baseline: in.Baseline, Actual: in.Actual, Inputs: map[string]any{}, Assumptions: []string{}}
	if !cal.Confirmed {
		res.Assumptions = append(res.Assumptions, "Working calendar not confirmed; assuming the configured working days and hours per day")
	}

	open, estimated := 0, 0
	var remaining float64
	for _, is := range in.Issues {
		if is.IsClosed {
			continue
		}
		open++
		if is.Estimated != nil {
			estimated++
			done := 0.0
			if is.DoneRatio != nil {
				done = float64(*is.DoneRatio) / 100
			}
			remaining += *is.Estimated * (1 - done)
		}
	}
	res.Inputs["open_issues"] = open
	res.Inputs["estimated_open_issues"] = estimated
	var coverage float64
	if open > 0 {
		coverage = float64(estimated) / float64(open)
		res.Inputs["estimate_coverage"] = round(coverage, 2)
	}
	if estimated > 0 {
		res.Inputs["remaining_estimated_hours"] = round(remaining, 1)
	}
	res.Inputs["weekly_capacity_hours"] = nullIfZero(in.WeeklyCapacity)

	closed, weeksWithCloses := throughput(in.Issues, in.Today)
	res.Inputs["closed_last_8_weeks"] = closed
	perWeek := float64(closed) / historyWeeks
	res.Inputs["closed_per_week"] = round(perWeek, 2)

	switch {
	case in.Actual != nil:
		res.Status = StatusDelivered
		res.Method = MethodNone
		res.Confidence = "high"
		res.Forecast = in.Actual
		return res
	case open == 0:
		res.Method = MethodNone
		res.Confidence = "none"
		res.Status = StatusTBD
		res.Assumptions = append(res.Assumptions, "No open issues in scope; nothing to forecast until delivery is recorded")
		return res
	}

	var fc *time.Time
	switch {
	case coverage >= minCoverageEffort && in.WeeklyCapacity > 0:
		res.Method = MethodEffort
		scaled := remaining / coverage
		if coverage < 1 {
			res.Assumptions = append(res.Assumptions,
				fmt.Sprintf("%d unestimated issue(s) assumed to be the same size on average as estimated ones", open-estimated))
		}
		res.Assumptions = append(res.Assumptions, "Remaining effort reduced by each issue's done ratio")
		hoursPerWorkingDay := in.WeeklyCapacity / math.Max(1, float64(len(cal.WorkingDays)))
		days := int(math.Ceil(scaled / hoursPerWorkingDay))
		res.Inputs["scaled_remaining_hours"] = round(scaled, 1)
		res.Inputs["working_days_needed"] = days
		t := cal.AddWorkingDays(in.Today, days)
		fc = &t
		switch {
		case coverage >= 0.8:
			res.Confidence = "high"
		default:
			res.Confidence = "medium"
		}
	case closed >= minClosedForTrend && weeksWithCloses >= minWeeksWithCloses:
		res.Method = MethodThroughput
		weeks := float64(open) / perWeek
		days := int(math.Ceil(weeks * float64(len(cal.WorkingDays))))
		res.Inputs["working_days_needed"] = days
		res.Assumptions = append(res.Assumptions, "Open issues close at the average rate of the last 8 weeks",
			"Issue count is a rough proxy; issues are not the same size")
		t := cal.AddWorkingDays(in.Today, days)
		fc = &t
		res.Confidence = "low"
		if weeksWithCloses >= 6 && closed >= 10 {
			res.Confidence = "medium"
		}
	default:
		res.Method = MethodNone
		res.Confidence = "none"
		res.Status = StatusTBD
		why := "fewer than half the open issues have estimates and no approved capacity is recorded"
		if coverage >= minCoverageEffort {
			why = "no approved capacity is recorded for this project"
		}
		res.Assumptions = append(res.Assumptions,
			fmt.Sprintf("Forecast is TBD: %s, and fewer than %d issues closed in the last 8 weeks", why, minClosedForTrend))
		return res
	}

	// A dependency outside the scope cannot finish before its own due date.
	sort.Slice(in.BlockerDue, func(i, j int) bool { return in.BlockerDue[i].After(in.BlockerDue[j]) })
	if len(in.BlockerDue) > 0 && in.BlockerDue[0].After(*fc) {
		t := in.BlockerDue[0]
		fc = &t
		res.Assumptions = append(res.Assumptions, "Forecast moved to the due date of an external blocking issue")
	}
	res.Forecast = fc

	res.Status = StatusTBD
	if in.Baseline != nil {
		res.Status = StatusOnTrack
		if fc.After(*in.Baseline) {
			res.Status = StatusAtRisk
			late := cal.WorkingDaysBetween(*in.Baseline, *fc)
			res.Flags = append(res.Flags, risks.Flag{Kind: risks.KindForecastLate, Severity: risks.High,
				Title:    fmt.Sprintf("Forecast %s is after the approved baseline %s", fc.Format(time.DateOnly), in.Baseline.Format(time.DateOnly)),
				Evidence: fmt.Sprintf("%d working day(s) late by %s; confidence %s", late, res.Method, res.Confidence),
				Action:   "Decide with the project manager: reduce scope, add capacity, or approve a revised baseline with a reason."})
		}
		if in.CapacityUntilBaseline >= 0 && estimated > 0 {
			need := remaining / math.Max(coverage, 0.01)
			res.Inputs["capacity_until_baseline_hours"] = round(in.CapacityUntilBaseline, 1)
			if need > in.CapacityUntilBaseline {
				res.Flags = append(res.Flags, risks.Flag{Kind: risks.KindCapacity, Severity: risks.High,
					Title:    "Remaining effort exceeds approved capacity before the baseline",
					Evidence: fmt.Sprintf("About %.0f h of work left, %.0f h of approved capacity until %s", need, in.CapacityUntilBaseline, in.Baseline.Format(time.DateOnly)),
					Action:   "Review allocations or the baseline."})
			}
		}
	} else {
		res.Assumptions = append(res.Assumptions, "No approved baseline, so the forecast cannot be compared with a commitment")
	}
	if coverage < 1 && res.Confidence == "high" {
		res.Confidence = "medium"
	}
	return res
}

// throughput counts issues closed in the last historyWeeks weeks and how
// many of those weeks had at least one closure.
func throughput(issues []risks.Issue, today time.Time) (closed, weeks int) {
	start := today.AddDate(0, 0, -7*historyWeeks)
	seen := map[int]bool{}
	for _, is := range issues {
		if !is.IsClosed || is.ClosedOn == nil || is.ClosedOn.Before(start) || is.ClosedOn.After(today.AddDate(0, 0, 1)) {
			continue
		}
		closed++
		seen[int(is.ClosedOn.Sub(start).Hours()/(24*7))] = true
	}
	return closed, len(seen)
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func nullIfZero(v float64) any {
	if v == 0 {
		return nil
	}
	return round(v, 1)
}
