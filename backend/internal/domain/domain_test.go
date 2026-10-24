package domain

import (
	"math"
	"testing"
)

func TestComputePrice(t *testing.T) {
	// 2 × KES 2,500 plus a KES 500 placement fee: KES 5,500, VAT included, nothing added on top.
	p := ComputePrice(250000, 2, 50000, 16)
	if p.TotalMinor != 550000 || p.ServiceMinor != 500000 || p.RoundingMinor != 0 {
		t.Fatalf("got %+v", p)
	}
	// 5,500 × 16/116 = 758.62
	if p.VATMinor != 75862 || p.SubtotalMinor != 474138 || p.SubtotalMinor+p.VATMinor != p.TotalMinor {
		t.Fatalf("VAT: got %+v", p)
	}
	// The QA case: a KES 999 price is KES 999, not 999 + 159.84.
	if p := ComputePrice(99900, 1, 0, 16); p.TotalMinor != 99900 || p.VATMinor != 13779 {
		t.Fatalf("999: got %+v", p)
	}
	// A fractional quantity is priced to the cent, then the total rounds to a whole shilling.
	p = ComputePrice(33350, 1.5, 0, 16) // 500.25
	if p.ServiceMinor != 50025 || p.TotalMinor != 50000 || p.RoundingMinor != -25 {
		t.Fatalf("rounding down: got %+v", p)
	}
	p = ComputePrice(33367, 1.5, 0, 16) // 500.505 -> 500.51
	if p.TotalMinor != 50100 || p.RoundingMinor != 49 {
		t.Fatalf("rounding up: got %+v", p)
	}
	// No VAT configured: the total is all subtotal.
	if p := ComputePrice(10000, 1, 0, 0); p.VATMinor != 0 || p.SubtotalMinor != 10000 {
		t.Fatalf("zero VAT: got %+v", p)
	}
}

func TestShillingsAndVAT(t *testing.T) {
	for _, c := range []struct{ in, want int64 }{{0, 0}, {49, 0}, {50, 100}, {115884, 115900}, {115849, 115800}, {-50, -100}} {
		if got := RoundShilling(c.in); got != c.want {
			t.Errorf("RoundShilling(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	// Every whole-shilling total splits exactly into net + VAT.
	for total := int64(100); total <= 1000000; total += 100 {
		if NetOfVAT(total, 16)+VATIncluded(total, 16) != total {
			t.Fatalf("split of %d does not add up", total)
		}
	}
	if VATIncluded(232000, 16) != 32000 || NetOfVAT(232000, 16) != 200000 {
		t.Fatal("2,320 should be 2,000 + 320 VAT")
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
