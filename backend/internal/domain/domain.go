// Package domain holds the business rules that must not depend on HTTP or SQL, so the HTML
// handlers and JSON API apply them identically (ADR-0001).
package domain

import (
	"math"
	"time"

	"github.com/KING-CYBERTON/home-residency/backend/internal/httpx"
)

// ---- pricing

type Price struct {
	SubtotalMinor int64
	VATMinor      int64
	TotalMinor    int64
}

// ComputePrice prices quantity units from the rate card. Amounts are integer cents, rounded half up.
func ComputePrice(unitMinor int64, quantity float64, placementFeeMinor int64, vatPct float64) Price {
	sub := int64(math.Round(float64(unitMinor)*quantity)) + placementFeeMinor
	vat := int64(math.Round(float64(sub) * vatPct / 100))
	return Price{SubtotalMinor: sub, VATMinor: vat, TotalMinor: sub + vat}
}

// ---- geography

// DistanceMetres is the great-circle distance between two points.
func DistanceMetres(lat1, lng1, lat2, lng2 float64) float64 {
	const r = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLng := rad(lat2-lat1), rad(lng2-lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * r * math.Asin(math.Sqrt(a))
}

// ---- request lifecycle (report §5.2)

func CanClientCancel(status string) bool {
	switch status {
	case "submitted", "triaging", "awaiting_client_choice", "awaiting_provider", "confirmed":
		return true
	}
	return false
}

func CanTriage(status string) bool {
	switch status {
	case "submitted", "triaging", "awaiting_client_choice", "awaiting_provider":
		return true
	}
	return false
}

// ---- assignment eligibility (report §5.1 step 10: only approved, trained providers are assignable)

type ProviderFacts struct {
	Status                string // providers.status
	CategoryStatus        string // provider_categories.status for the request's category ("" if none)
	ExpiredMandatoryDocs  int
	OpenSafeguardingCases int
	LiveJobs              int
	MaxConcurrentJobs     int
	BusyAtRequestedTime   bool
}

func CheckAssignable(f ProviderFacts) *httpx.Error {
	switch {
	case f.Status != "active":
		return httpx.RuleViolation("Provider is not active (status: " + f.Status + ")")
	case f.CategoryStatus != "active":
		return httpx.RuleViolation("Provider is not activated for this service category")
	case f.ExpiredMandatoryDocs > 0:
		return httpx.RuleViolation("Provider has a mandatory document that is expired")
	case f.OpenSafeguardingCases > 0:
		return httpx.RuleViolation("Provider has an open safeguarding or loss case")
	case f.BusyAtRequestedTime:
		return httpx.RuleViolation("Provider already has a job or time off at the requested time")
	case f.MaxConcurrentJobs > 0 && f.LiveJobs >= f.MaxConcurrentJobs:
		return httpx.RuleViolation("Provider is at their maximum number of concurrent jobs")
	}
	return nil
}

// ---- ratings

// OpensComplaint reports whether a rating must open a complaint case automatically (§5.4 step 1).
func OpensComplaint(stars int, threshold float64) bool { return float64(stars) <= threshold }

// ---- shortlist scoring (the engine proposes; a human assigns)

type MatchInput struct {
	DistanceKm   float64
	RatingAvg    float64 // 0 when unrated
	RatingCount  int
	LiveJobs     int
	MaxJobs      int
	CompletedJob int
}

type MatchScore struct {
	Score        float64
	Skill        float64
	DistanceKm   float64
	Rating       float64
	Availability float64
	Workload     float64
}

// ScoreMatch weights experience, distance, rating and spare capacity. Weights are fixed here and
// returned in the breakdown so triage officers can see why a provider ranked where they did.
func ScoreMatch(in MatchInput) MatchScore {
	skill := math.Min(float64(in.CompletedJob)/50, 1)
	dist := 1 - math.Min(in.DistanceKm/15, 1)
	rating := 0.6 // neutral prior for new providers
	if in.RatingCount > 0 {
		rating = in.RatingAvg / 5
	}
	maxJobs := math.Max(float64(in.MaxJobs), 1)
	workload := 1 - math.Min(float64(in.LiveJobs)/maxJobs, 1)
	score := 0.2*skill + 0.3*dist + 0.3*rating + 0.2*workload
	return MatchScore{Score: math.Round(score*1000) / 1000, Skill: skill, DistanceKm: in.DistanceKm, Rating: rating, Availability: 1, Workload: workload}
}

// DefaultVisitLength is used when a request has no end time.
const DefaultVisitLength = 3 * time.Hour
