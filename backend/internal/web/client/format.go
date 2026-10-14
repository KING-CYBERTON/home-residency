package client

import (
	"fmt"
	"html/template"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Households are in Kenya: show and read times in East Africa Time (no daylight saving).
var eat = time.FixedZone("EAT", 3*60*60)

func (p *Portal) funcs() template.FuncMap {
	return template.FuncMap{
		"kes":   money,
		"when":  when,
		"day":   func(t time.Time) string { return t.In(eat).Format("Mon 2 Jan 2006") },
		"whenp": func(t *time.Time) string { return whenPtr(t) },
		"str":   func(s *string) string { return deref(s) },
		"label": label,
		"tone":  tone,
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i + 1
			}
			return s
		},
		"due": func(total, paid int64) int64 { return total - paid },
		"nowLocal": func() string {
			return time.Now().In(eat).Add(2 * time.Hour).Truncate(time.Hour).Format("2006-01-02T15:04")
		},
		// The earliest start, rounded up to the quarter hour so the input's 15-minute steps line up.
		"minLocal": func() string {
			return time.Now().In(eat).Add(15 * time.Minute).Truncate(15 * time.Minute).Format("2006-01-02T15:04")
		},
		"dict":    dict,
		"phone":   localPhone,
		"deref64": deref64,
		"val":     derefAny,
		"deref32": func(f *float32) float64 {
			if f == nil {
				return 0
			}
			return float64(*f)
		},
		"list":  func(s ...string) []string { return s },
		"minus": func(a, b any) int64 { return deref64(a) - deref64(b) },
		"per":   func(u any) string { return strings.TrimPrefix(fmt.Sprint(u), "per_") },
	}
}

func dict(kv ...any) map[string]any {
	m := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[fmt.Sprint(kv[i])] = kv[i+1]
	}
	return m
}

// money formats KES cents as "KES 2,500" (or "KES 2,500.50" when there are cents).
func money(v any) string {
	var minor int64
	switch x := v.(type) {
	case int64:
		minor = x
	case *int64:
		if x == nil {
			return ""
		}
		minor = *x
	case int:
		minor = int64(x)
	}
	neg := minor < 0
	if neg {
		minor = -minor
	}
	whole, cents := minor/100, minor%100
	s := strconv.FormatInt(whole, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if cents != 0 {
		s += fmt.Sprintf(".%02d", cents)
	}
	if neg {
		s = "−" + s
	}
	return "KES " + s
}

// derefAny prints what a pointer points at, or nothing for nil.
func derefAny(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return ""
		}
		return rv.Elem().Interface()
	}
	return v
}

func deref64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case *int64:
		if x != nil {
			return *x
		}
	}
	return 0
}

func when(t time.Time) string { return t.In(eat).Format("Mon 2 Jan, 15:04") }

func whenPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return when(*t)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// labels turns status enums into words a household understands.
var labels = map[string]string{
	"submitted": "Received", "triaging": "Finding a provider", "awaiting_client_choice": "Choose your provider",
	"awaiting_provider": "Waiting for the provider", "confirmed": "Confirmed", "in_progress": "In progress",
	"awaiting_rating": "Please rate this job", "closed": "Done", "cancelled": "Cancelled", "unfilled": "No provider found",
	"scheduled": "Scheduled", "checked_in": "Provider has arrived", "completed": "Finished", "client_confirmed": "You confirmed",
	"missed": "Missed",
	"draft":  "Draft", "issued": "To pay", "partially_paid": "Part paid", "paid": "Paid", "void": "Cancelled", "refunded": "Refunded",
	"initiated": "Sending prompt", "pending": "Waiting for you to approve on your phone", "succeeded": "Paid", "failed": "Failed", "reversed": "Reversed",
	"open": "Received", "investigating": "Under investigation", "awaiting_provider_account": "Under investigation",
	"awaiting_decision": "Awaiting decision", "decided": "Decided", "withdrawn": "Withdrawn",
	"approved": "Approved", "rejected": "Declined",
	"quality": "Quality of work", "conduct": "Behaviour", "safeguarding": "Safety or abuse", "loss": "Loss or damage",
	"none": "Not verified", "verified": "Verified",
	"active": "Active", "past_due": "Awaiting first payment",
}

func label(v any) string {
	s := fmt.Sprint(v)
	if l, ok := labels[s]; ok {
		return l
	}
	return strings.ReplaceAll(s, "_", " ")
}

// tone picks the badge colour for a status.
func tone(v any) string {
	switch fmt.Sprint(v) {
	case "closed", "paid", "succeeded", "client_confirmed", "verified", "approved", "active", "confirmed", "decided":
		return "good"
	case "awaiting_client_choice", "awaiting_rating", "issued", "partially_paid", "pending", "completed", "past_due":
		return "act"
	case "cancelled", "unfilled", "failed", "void", "rejected", "missed", "reversed":
		return "bad"
	}
	return "info"
}

// phoneE164 accepts the ways Kenyans write numbers (0712…, 712…, 254712…, +254712…).
func phoneE164(in string) string {
	s := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, in)
	switch {
	case strings.HasPrefix(s, "+"):
		return s
	case strings.HasPrefix(s, "254"):
		return "+" + s
	case strings.HasPrefix(s, "0") && len(s) == 10:
		return "+254" + s[1:]
	case len(s) == 9 && (s[0] == '7' || s[0] == '1'):
		return "+254" + s
	}
	return s
}

// localPhone shows +254712345678 as 0712 345 678.
func localPhone(e164 string) string {
	if strings.HasPrefix(e164, "+254") && len(e164) == 13 {
		s := "0" + e164[4:]
		return s[:4] + " " + s[4:7] + " " + s[7:]
	}
	return e164
}

// parseLocal reads an <input type="datetime-local"> value as East Africa Time.
func parseLocal(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02T15:04", s, eat)
	return t, err == nil
}

func parseUUID(s string) (openapi_types.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(s))
	return openapi_types.UUID(id), err == nil
}

func formUUID(r *http.Request, key string) (openapi_types.UUID, bool) {
	return parseUUID(r.PostFormValue(key))
}

func pathUUID(r *http.Request, key string) (openapi_types.UUID, bool) {
	return parseUUID(chiParam(r, key))
}

func optString(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}
