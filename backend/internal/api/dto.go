package api

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Response shapes. Field names and nullability follow components/schemas in openapi.yaml;
// contract_test.go validates real responses against the spec so these cannot drift.

type meDTO struct {
	ID                uuid.UUID `json:"id"`
	Kind              string    `json:"kind"`
	Status            string    `json:"status"`
	FullName          string    `json:"full_name"`
	Phone             *string   `json:"phone"`
	Email             *string   `json:"email"`
	PreferredLanguage string    `json:"preferred_language"`
	CreatedAt         time.Time `json:"created_at"`
	Roles             []string  `json:"roles"`
	ProviderStatus    *string   `json:"provider_status"`
	IDVerification    *string   `json:"id_verification"`
	HasPassword       bool      `json:"has_password"`
}

type sessionDTO struct {
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expires_at"`
	MFARequired bool      `json:"mfa_required"`
	User        meDTO     `json:"user"`
}

type serviceAreaDTO struct {
	ID        uuid.UUID `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	County    string    `json:"county"`
	IsActive  bool      `json:"is_active"`
	CentreLat *float64  `json:"centre_lat"`
	CentreLng *float64  `json:"centre_lng"`
	RadiusKm  *float64  `json:"radius_km"`
}

type categoryDTO struct {
	ID                           uuid.UUID `json:"id"`
	Slug                         string    `json:"slug"`
	Name                         string    `json:"name"`
	NameSw                       *string   `json:"name_sw"`
	Description                  *string   `json:"description"`
	Engagement                   string    `json:"engagement"`
	VettingTier                  string    `json:"vetting_tier"`
	RequiresClientIDVerification bool      `json:"requires_client_id_verification"`
	SopMarkdown                  *string   `json:"sop_markdown"`
	IsActive                     bool      `json:"is_active"`
	SortOrder                    int       `json:"sort_order"`
	OpensAt                      *string   `json:"opens_at"`  // earliest start, HH:MM Nairobi time; nil means any time
	ClosesAt                     *string   `json:"closes_at"` // latest start
	MinLeadMinutes               int       `json:"min_lead_minutes"`
}

type rateCardEntryDTO struct {
	ID                uuid.UUID  `json:"id"`
	ServiceID         uuid.UUID  `json:"service_id"`
	ServiceAreaID     *uuid.UUID `json:"service_area_id"`
	ClientPriceMinor  int64      `json:"client_price_minor"`
	ProviderPayMinor  int64      `json:"provider_pay_minor"`
	PlacementFeeMinor int64      `json:"placement_fee_minor"`
	Currency          string     `json:"currency"`
	ValidFrom         time.Time  `json:"valid_from"`
	ValidTo           *time.Time `json:"valid_to"`
}

type serviceDTO struct {
	ID                     uuid.UUID         `json:"id"`
	CategoryID             uuid.UUID         `json:"category_id"`
	Slug                   string            `json:"slug"`
	Name                   string            `json:"name"`
	Description            *string           `json:"description"`
	ScopeItems             []string          `json:"scope_items"`
	PricingUnit            string            `json:"pricing_unit"`
	MinQuantity            float64           `json:"min_quantity"`
	MaxQuantity            *float64          `json:"max_quantity"`
	DefaultDurationMinutes *int              `json:"default_duration_minutes"`
	IsActive               bool              `json:"is_active"`
	CurrentPrice           *rateCardEntryDTO `json:"current_price"`
}

type faqDTO struct {
	ID          uuid.UUID  `json:"id"`
	CategoryID  *uuid.UUID `json:"category_id"`
	Language    string     `json:"language"`
	Question    string     `json:"question"`
	Answer      string     `json:"answer"`
	SortOrder   int        `json:"sort_order"`
	IsPublished bool       `json:"is_published"`
}

type planDTO struct {
	ID            uuid.UUID      `json:"id"`
	Code          string         `json:"code"`
	Name          string         `json:"name"`
	Description   *string        `json:"description"`
	PriceMinor    int64          `json:"price_minor"`
	BillingPeriod string         `json:"billing_period"`
	Benefits      map[string]any `json:"benefits"`
	IsActive      bool           `json:"is_active"`
}

type addressDTO struct {
	ID            uuid.UUID  `json:"id"`
	Label         string     `json:"label"`
	ServiceAreaID *uuid.UUID `json:"service_area_id"`
	Estate        *string    `json:"estate"`
	Street        *string    `json:"street"`
	HouseNumber   *string    `json:"house_number"`
	Landmark      *string    `json:"landmark"`
	Lat           *float64   `json:"lat"`
	Lng           *float64   `json:"lng"`
	AccessDetails *string    `json:"access_details"`
	IsDefault     bool       `json:"is_default"`
}

type quoteDTO struct {
	ServiceID            uuid.UUID `json:"service_id"`
	RateCardEntryID      uuid.UUID `json:"rate_card_entry_id"`
	Quantity             float64   `json:"quantity"`
	UnitPriceMinor       int64     `json:"unit_price_minor"`
	PlacementFeeMinor    int64     `json:"placement_fee_minor"`
	ServiceMinor         int64     `json:"service_minor"`
	RoundingMinor        int64     `json:"rounding_minor"`
	SubtotalMinor        int64     `json:"subtotal_minor"`
	VATMinor             int64     `json:"vat_minor"`
	TotalMinor           int64     `json:"total_minor"`
	Currency             string    `json:"currency"`
	ServiceAreaSupported bool      `json:"service_area_supported"`
}

type providerPublicDTO struct {
	ID              uuid.UUID `json:"id"`
	DisplayName     string    `json:"display_name"`
	PhotoURL        *string   `json:"photo_url"`
	Bio             *string   `json:"bio"`
	YearsExperience *int      `json:"years_experience"`
	Languages       []string  `json:"languages"`
	RatingAvg       *float64  `json:"rating_avg"`
	RatingCount     int       `json:"rating_count"`
	CompletedJobs   int       `json:"completed_jobs"`
	Categories      []string  `json:"categories"`
}

type providerSummaryDTO struct {
	ID                uuid.UUID  `json:"id"`
	FullName          string     `json:"full_name"`
	Status            string     `json:"status"`
	Phone             *string    `json:"phone"`
	RatingAvg         *float64   `json:"rating_avg"`
	AppraisalScore    *float64   `json:"appraisal_score"`
	CompletedJobs     int        `json:"completed_jobs"`
	HomeServiceAreaID *uuid.UUID `json:"home_service_area_id"`
}

type visitDTO struct {
	ID                uuid.UUID  `json:"id"`
	RequestID         uuid.UUID  `json:"request_id"`
	Status            string     `json:"status"`
	ScheduledStart    time.Time  `json:"scheduled_start"`
	ScheduledEnd      time.Time  `json:"scheduled_end"`
	CheckedInAt       *time.Time `json:"checked_in_at"`
	CheckinMethod     *string    `json:"checkin_method"`
	CheckedOutAt      *time.Time `json:"checked_out_at"`
	ClientConfirmedAt *time.Time `json:"client_confirmed_at"`
	ProviderNotes     *string    `json:"provider_notes"`
}

type requestDTO struct {
	ID                uuid.UUID          `json:"id"`
	Reference         string             `json:"reference"`
	Status            string             `json:"status"`
	ClientID          uuid.UUID          `json:"client_id"`
	Address           addressDTO         `json:"address"`
	Category          categoryDTO        `json:"category"`
	Service           serviceDTO         `json:"service"`
	Quantity          float64            `json:"quantity"`
	ClientPriceMinor  int64              `json:"client_price_minor"`
	PlacementFeeMinor int64              `json:"placement_fee_minor"`
	TotalMinor        int64              `json:"total_minor"`
	Currency          string             `json:"currency"`
	RequestedStart    time.Time          `json:"requested_start"`
	RequestedEnd      *time.Time         `json:"requested_end"`
	RecurrenceRule    *string            `json:"recurrence_rule"`
	ClientNotes       *string            `json:"client_notes"`
	OfferClientChoice bool               `json:"offer_client_choice"`
	Provider          *providerPublicDTO `json:"provider"`
	Visits            []visitDTO         `json:"visits"`
	InvoiceID         *uuid.UUID         `json:"invoice_id"`
	RatingRequired    bool               `json:"rating_required"`
	SubmittedAt       time.Time          `json:"submitted_at"`
	ConfirmedAt       *time.Time         `json:"confirmed_at"`
	CompletedAt       *time.Time         `json:"completed_at"`
	ClosedAt          *time.Time         `json:"closed_at"`
	CancelledAt       *time.Time         `json:"cancelled_at"`
	CancelledBy       *string            `json:"cancelled_by"`
	CancelReason      *string            `json:"cancel_reason"`
}

type clientSummaryDTO struct {
	ID             uuid.UUID `json:"id"`
	FullName       string    `json:"full_name"`
	Phone          *string   `json:"phone"`
	Status         string    `json:"status"`
	IDVerification string    `json:"id_verification"`
	CreatedAt      time.Time `json:"created_at"`
}

type assignmentDTO struct {
	ID              uuid.UUID          `json:"id"`
	RequestID       uuid.UUID          `json:"request_id"`
	Provider        providerSummaryDTO `json:"provider"`
	Status          string             `json:"status"`
	SelectedBy      uuid.UUID          `json:"selected_by"`
	SelectionReason string             `json:"selection_reason"`
	OfferedAt       time.Time          `json:"offered_at"`
	OfferExpiresAt  time.Time          `json:"offer_expires_at"`
	RespondedAt     *time.Time         `json:"responded_at"`
	ResponseChannel *string            `json:"response_channel"`
	DeclineReason   *string            `json:"decline_reason"`
}

type requestStaffDTO struct {
	requestDTO
	Client           clientSummaryDTO  `json:"client"`
	ProviderPayMinor int64             `json:"provider_pay_minor"`
	Priority         int               `json:"priority"`
	TriageOfficerID  *uuid.UUID        `json:"triage_officer_id"`
	Assignments      []assignmentDTO   `json:"assignments"`
	Variations       []json.RawMessage `json:"variations"`
	Exceptions       []json.RawMessage `json:"exceptions"`
	TriageStartedAt  *time.Time        `json:"triage_started_at"`
}

type shortlistEntryDTO struct {
	ID             uuid.UUID          `json:"id"`
	Rank           int                `json:"rank"`
	Score          float64            `json:"score"`
	ScoreBreakdown map[string]float64 `json:"score_breakdown"`
	Provider       providerSummaryDTO `json:"provider"`
	ShownToClient  bool               `json:"shown_to_client"`
	ClientSelected bool               `json:"client_selected"`
	GeneratedAt    time.Time          `json:"generated_at"`
}

type offerDTO struct {
	ID               uuid.UUID       `json:"id"`
	Status           string          `json:"status"`
	OfferExpiresAt   time.Time       `json:"offer_expires_at"`
	RequestReference string          `json:"request_reference"`
	Service          serviceDTO      `json:"service"`
	Estate           *string         `json:"estate"`
	ServiceArea      *serviceAreaDTO `json:"service_area"`
	RequestedStart   time.Time       `json:"requested_start"`
	RequestedEnd     *time.Time      `json:"requested_end"`
	RecurrenceRule   *string         `json:"recurrence_rule"`
	ProviderPayMinor int64           `json:"provider_pay_minor"`
	ClientNotes      *string         `json:"client_notes"`
}

type ratingDTO struct {
	ID          uuid.UUID      `json:"id"`
	RequestID   uuid.UUID      `json:"request_id"`
	VisitID     *uuid.UUID     `json:"visit_id"`
	ProviderID  uuid.UUID      `json:"provider_id"`
	Stars       int            `json:"stars"`
	SubScores   map[string]int `json:"sub_scores"`
	Comment     *string        `json:"comment"`
	CreatedAt   time.Time      `json:"created_at"`
	ComplaintID *uuid.UUID     `json:"complaint_id"`
}
