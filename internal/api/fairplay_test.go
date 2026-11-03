package api

import (
	"testing"
	"time"
)

func row(team int64, chal int64, flag string, at time.Time) fairPlayRow {
	return fairPlayRow{TeamID: team, TeamName: "t" + string(rune('0'+team)), ChallengeID: chal, ChallengeName: "Stack Smasher", Submitted: flag, SubmittedAt: at}
}

func TestSharedFlagAlerts(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	rows := []fairPlayRow{
		row(1, 7, "flag{leaked}", t0),
		row(2, 7, "flag{leaked}", t0.Add(40*time.Second)),
		row(2, 7, "flag{leaked}", t0.Add(50*time.Second)), // same team again: still 2 teams
		row(3, 7, "flag{leaked}", t0.Add(2*time.Minute)),
		// The same wrong flag on another challenge doesn't count toward it.
		row(4, 8, "flag{leaked}", t0.Add(3*time.Minute)),
		// A common guess, but spread over an hour: not an alert.
		row(1, 7, "flag{test}", t0),
		row(2, 7, "flag{test}", t0.Add(30*time.Minute)),
		row(3, 7, "flag{test}", t0.Add(60*time.Minute)),
	}
	got := sharedFlagAlerts(rows)
	if len(got) != 1 {
		t.Fatalf("alerts = %+v", got)
	}
	a := got[0]
	if a.Kind != "shared-flag" || a.Count != 3 || len(a.Teams) != 3 || a.SpanSeconds != 120 || a.ChallengeID != 7 || !a.At.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("alert = %+v", a)
	}

	// Two teams aren't enough.
	if got := sharedFlagAlerts(rows[:3]); len(got) != 0 {
		t.Fatalf("two teams raised %+v", got)
	}
}

func TestRateAlerts(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	var rows []fairPlayRow
	// Team 1: 8 attempts in 50 seconds (limit 10, alert at 8), mostly on challenge 5.
	for i := 0; i < 8; i++ {
		chal := int64(5)
		if i == 0 {
			chal = 6
		}
		rows = append(rows, row(1, chal, "x", t0.Add(time.Duration(i*7)*time.Second)))
	}
	// Team 2: 8 attempts, but spread over 4 minutes.
	for i := 0; i < 8; i++ {
		rows = append(rows, row(2, 5, "x", t0.Add(time.Duration(i*30)*time.Second)))
	}
	sortRows(rows)
	got := rateAlerts(rows, 10)
	if len(got) != 1 || got[0].TeamIDs[0] != 1 || got[0].Count != 8 || got[0].Limit != 10 || got[0].ChallengeID != 5 {
		t.Fatalf("alerts = %+v", got)
	}
	if got := rateAlerts(rows, 20); len(got) != 0 {
		t.Fatalf("limit 20 raised %+v", got)
	}
}

func sortRows(rows []fairPlayRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].SubmittedAt.Before(rows[j-1].SubmittedAt); j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}
