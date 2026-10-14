package domain

import (
	"math"
	"testing"
)

func TestComputePrice(t *testing.T) {
	p := ComputePrice(250000, 2, 50000, 16)
	if p.SubtotalMinor != 550000 || p.VATMinor != 88000 || p.TotalMinor != 638000 {
		t.Fatalf("got %+v", p)
	}
	// fractional quantity rounds half up to the cent
	if got := ComputePrice(333, 1.5, 0, 0).SubtotalMinor; got != 500 {
		t.Fatalf("got %d", got)
	}
}

func TestDistance(t *testing.T) {
	// Kilimani to Westlands is roughly 4 km
	d := DistanceMetres(-1.2921, 36.7872, -1.2676, 36.8108)
	if d < 3000 || d > 4500 {
		t.Fatalf("distance %f", d)
	}
	if DistanceMetres(1, 1, 1, 1) != 0 {
		t.Fatal("same point should be 0")
	}
}

func TestCheckAssignable(t *testing.T) {
	ok := ProviderFacts{Status: "active", CategoryStatus: "active", MaxConcurrentJobs: 2}
	if err := CheckAssignable(ok); err != nil {
		t.Fatal(err)
	}
	cases := map[string]ProviderFacts{
		"suspended":    {Status: "suspended", CategoryStatus: "active"},
		"not category": {Status: "active", CategoryStatus: "pending"},
		"expired doc":  {Status: "active", CategoryStatus: "active", ExpiredMandatoryDocs: 1},
		"safeguarding": {Status: "active", CategoryStatus: "active", OpenSafeguardingCases: 1},
		"busy":         {Status: "active", CategoryStatus: "active", BusyAtRequestedTime: true},
		"at capacity":  {Status: "active", CategoryStatus: "active", LiveJobs: 2, MaxConcurrentJobs: 2},
	}
	for name, f := range cases {
		if err := CheckAssignable(f); err == nil || err.Code != "rule_violation" {
			t.Errorf("%s: expected rule_violation, got %v", name, err)
		}
	}
}

func TestOpensComplaint(t *testing.T) {
	if !OpensComplaint(3, 3) || !OpensComplaint(1, 3) || OpensComplaint(4, 3) {
		t.Fatal("threshold logic wrong")
	}
}

func TestScoreMatch(t *testing.T) {
	near := ScoreMatch(MatchInput{DistanceKm: 1, RatingAvg: 4.8, RatingCount: 20, MaxJobs: 2, CompletedJob: 40})
	far := ScoreMatch(MatchInput{DistanceKm: 14, RatingAvg: 4.8, RatingCount: 20, MaxJobs: 2, CompletedJob: 40})
	if near.Score <= far.Score {
		t.Fatal("nearer provider should rank higher")
	}
	if math.IsNaN(ScoreMatch(MatchInput{}).Score) {
		t.Fatal("zero input produced NaN")
	}
}
