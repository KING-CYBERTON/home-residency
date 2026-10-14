package provider

import (
	"fmt"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

// eat is Kenya's time zone (UTC+3, no daylight saving). Every time on a page is shown in it.
var eat = time.FixedZone("EAT", 3*3600)

var funcs = template.FuncMap{
	"kes":      kes,
	"when":     func(t any) string { return fmtTime(t, "Mon 2 Jan, 15:04") },
	"day":      func(t any) string { return fmtTime(t, "Mon 2 Jan 2006") },
	"clock":    func(t any) string { return fmtTime(t, "15:04") },
	"isoLocal": func(t any) string { return fmtTime(t, "2006-01-02T15:04") },
	"label":    label,
	"val":      deref,
	"has":      func(v any) bool { return deref(v) != nil && deref(v) != "" },
	"newID":    func() string { return uuid.NewString() },
	"pct": func(f any) string {
		v, ok := deref(f).(float32)
		if !ok {
			return "–"
		}
		return strconv.Itoa(int(v*100+0.5)) + "%"
	},
	"score": func(f any) string {
		v, ok := deref(f).(float32)
		if !ok {
			return "–"
		}
		return strconv.FormatFloat(float64(v), 'f', 1, 32)
	},
	"eq2": func(a, b any) bool { return fmt.Sprint(deref(a)) == fmt.Sprint(deref(b)) },
	"dict": func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	},
	"weekday": func(i int) string { return time.Weekday(i).String() },
	"seq": func(n int) []int {
		s := make([]int, n)
		for i := range s {
			s[i] = i
		}
		return s
	},
}

// kes is web.Money for the optional (pointer) money fields the API returns.
func kes(v any) string {
	var minor int64
	switch x := deref(v).(type) {
	case int64:
		minor = x
	case int:
		minor = int64(x)
	default:
		return "–"
	}
	return web.Money(minor)
}

func fmtTime(v any, layout string) string {
	switch t := deref(v).(type) {
	case time.Time:
		return t.In(eat).Format(layout)
	case openapi_types.Date:
		return t.Time.Format(strings.TrimSuffix(strings.TrimSuffix(layout, ", 15:04"), "T15:04"))
	}
	return ""
}

// label turns an enum value into words: "awaiting_provider_account" → "Awaiting provider account".
func label(v any) string {
	s := strings.ReplaceAll(fmt.Sprint(deref(v)), "_", " ")
	if s == "" || s == "<nil>" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// deref follows pointers so templates can treat optional API fields like plain values.
func deref(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	return rv.Interface()
}
