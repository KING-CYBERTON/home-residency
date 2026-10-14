package api_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJobFiltersAndConcurrentCheckIn(t *testing.T) {
	h := newHarness(t)
	w := h.newWorld()
	h.staff("triage", "tr@example.com", "triage_officer")
	count := func(status string) int {
		return len(h.expect(200, "provider", "GET", "/provider/jobs?status="+status, nil)["data"].([]any))
	}
	_, _, visit := h.accepted(w, "triage")
	if count("upcoming") != 1 || count("active") != 0 || count("past") != 0 {
		t.Fatalf("before check-in: %d/%d/%d", count("upcoming"), count("active"), count("past"))
	}

	// the same event arriving twice at once is applied once, and both calls succeed
	code := h.expect(200, "client", "GET", "/client/visits/"+visit+"/checkin-code", nil)["code"].(string)
	body := map[string]any{"client_event_id": uuid.NewString(), "method": "code", "code": code, "device_recorded_at": time.Now().UTC().Format(time.RFC3339)}
	var wg sync.WaitGroup
	got := make([]int, 4)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = h.call("provider", "POST", "/provider/visits/"+visit+"/check-in", body)
		}()
	}
	wg.Wait()
	for _, c := range got {
		if c != 200 {
			t.Fatalf("concurrent duplicates: %v", got)
		}
	}
	if count("upcoming") != 0 || count("active") != 1 {
		t.Fatalf("after check-in: %d/%d", count("upcoming"), count("active"))
	}
	h.checkOut(visit)
	if count("past") != 1 || count("active") != 0 {
		t.Fatalf("after check-out: %d/%d", count("past"), count("active"))
	}
}
