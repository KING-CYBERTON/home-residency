package api

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// settingRule says what value a known setting takes, so a typing slip in the console cannot store
// "90 minutes" where a number of minutes belongs and quietly break the rule that reads it.
type settingRule struct {
	kind     string // number, integer, text, weights, payout_cycle
	min, max float64
}

var settingRules = map[string]settingRule{
	"appraisal.weights":                {kind: "weights"},
	"checkin.max_distance_m":           {kind: "integer", min: 25, max: 5000},
	"commission.default_pct":           {kind: "number", min: 0, max: 100},
	"contract.code_of_conduct_version": {kind: "text"},
	"contract.current_version":         {kind: "text"},
	"document.expiry_warning_days":     {kind: "integer", min: 1, max: 365},
	"offer.expiry_minutes":             {kind: "integer", min: 5, max: 1440},
	"otp.max_attempts":                 {kind: "integer", min: 1, max: 20},
	"otp.ttl_minutes":                  {kind: "integer", min: 1, max: 60},
	"payout.cycle":                     {kind: "payout_cycle"},
	"rating.complaint_threshold":       {kind: "integer", min: 1, max: 5},
	"session.client_ttl_hours":         {kind: "integer", min: 1, max: 8760},
	"session.staff_ttl_hours":          {kind: "integer", min: 1, max: 24},
	"vat.rate_pct":                     {kind: "number", min: 0, max: 50},
}

// appraisalComponents are the parts of a provider's appraisal score that can be weighted.
var appraisalComponents = []string{"rating", "complaints", "reliability", "spot_checks"}

// checkSetting returns a message for the officer when value does not fit the setting, or "" when it does.
// Settings the console does not know about yet keep taking any JSON value.
func checkSetting(key string, value any) string {
	rule, ok := settingRules[key]
	if !ok {
		return ""
	}
	switch rule.kind {
	case "number", "integer":
		n, ok := value.(float64)
		if !ok {
			return fmt.Sprintf("%s takes a number", key)
		}
		if rule.kind == "integer" && n != math.Trunc(n) {
			return fmt.Sprintf("%s takes a whole number", key)
		}
		if n < rule.min || n > rule.max {
			return fmt.Sprintf("%s must be between %g and %g", key, rule.min, rule.max)
		}
	case "text":
		t, ok := value.(string)
		if !ok || strings.TrimSpace(t) == "" {
			return fmt.Sprintf("%s takes some text", key)
		}
	case "weights":
		m, ok := value.(map[string]any)
		if !ok {
			return "Give a weight for each part of the appraisal"
		}
		sum := 0.0
		for _, c := range appraisalComponents {
			n, ok := m[c].(float64)
			if !ok || n < 0 || n > 1 {
				return fmt.Sprintf("The %s weight must be between 0 and 1", strings.ReplaceAll(c, "_", " "))
			}
			sum += n
		}
		if len(m) != len(appraisalComponents) {
			return "The appraisal weights are rating, complaints, reliability and spot checks"
		}
		if math.Abs(sum-1) > 0.001 {
			return fmt.Sprintf("The appraisal weights must add up to 100%%; these add up to %.0f%%", sum*100)
		}
	case "payout_cycle":
		raw, _ := json.Marshal(value)
		var c struct {
			Frequency string   `json:"frequency"`
			Weekday   *float64 `json:"weekday"`
		}
		if json.Unmarshal(raw, &c) != nil || c.Frequency != "weekly" {
			return "Payouts run weekly; choose the day"
		}
		if c.Weekday == nil || *c.Weekday != math.Trunc(*c.Weekday) || *c.Weekday < 0 || *c.Weekday > 6 {
			return "Choose a payout day from Sunday to Saturday"
		}
	}
	return ""
}
