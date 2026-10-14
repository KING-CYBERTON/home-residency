package provider

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api"
	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

// Work (report §5.2): availability, offers, accepted jobs, check-in and check-out (online, offline
// through /sync, or by USSD/SMS through the same handlers), and scope variations.

// ---- dashboard

type dashboardPage struct {
	Me             gen.Me
	Status         gen.ProviderStatus
	Offers         []gen.Offer
	Today          []jobVisit
	Upcoming       []jobVisit
	Apps           []gen.ApplicationSummary
	Contract       *gen.Contract
	Earnings       *gen.EarningsSummary
	OpenComplaints int
}

// jobVisit is one visit with the job it belongs to, for schedule lists.
type jobVisit struct {
	Job   gen.Job
	Visit gen.Visit
}

func (p *Portal) dashboard(w http.ResponseWriter, r *http.Request) {
	me, err := call[gen.Me](r, nil, p.api.GetMe)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	d := dashboardPage{Me: me}
	if me.ProviderStatus != nil {
		d.Status = *me.ProviderStatus
	}
	switch d.Status {
	case gen.ProviderStatusApplicant, gen.ProviderStatusWithdrawn:
		d.Apps, _ = call[[]gen.ApplicationSummary](r, nil, p.api.GetProviderApplications)
	case gen.ProviderStatusApproved:
		if c, err := call[gen.Contract](r, nil, p.api.GetProviderContract); err == nil {
			d.Contract = &c
		}
	case gen.ProviderStatusActive:
		d.Offers, _ = call[[]gen.Offer](r, nil, p.api.GetProviderOffers)
		open, _ := p.openVisits(r)
		today := time.Now().In(eat).Format("2006-01-02")
		for _, jv := range open {
			if jv.Visit.ScheduledStart.In(eat).Format("2006-01-02") <= today {
				d.Today = append(d.Today, jv)
			} else if len(d.Upcoming) < 3 {
				d.Upcoming = append(d.Upcoming, jv)
			}
		}
		if s, err := call[gen.EarningsSummary](r, nil, p.api.GetProviderEarningsSummary); err == nil {
			d.Earnings = &s
		}
	}
	if cs, err := call[[]gen.Complaint](r, nil, p.api.GetProviderComplaints); err == nil {
		for _, c := range cs {
			if c.Status == gen.ComplaintStatusAwaitingProviderAccount {
				d.OpenComplaints++
			}
		}
	}
	p.render(w, r, http.StatusOK, page{name: "dashboard", title: "Home", nav: "home"}, d)
}

func (p *Portal) more(w http.ResponseWriter, r *http.Request) {
	me, err := call[gen.Me](r, nil, p.api.GetMe)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "more", title: "More", nav: "more"}, me)
}

// ---- jobs

// allJobs reads the provider's accepted jobs. The page size is the API maximum; a provider rarely has more.
func (p *Portal) allJobs(r *http.Request) ([]gen.Job, error) {
	limit := 100
	pg, err := call[gen.JobPage](r, nil, func(w http.ResponseWriter, rq *http.Request) {
		p.api.GetProviderJobs(w, rq, gen.GetProviderJobsParams{Limit: &limit})
	})
	return pg.Data, err
}

func openVisit(v gen.Visit) bool {
	return v.Status == gen.VisitStatusScheduled || v.Status == gen.VisitStatusCheckedIn
}

// openVisits is every visit still to do (or in progress), soonest first.
func (p *Portal) openVisits(r *http.Request) ([]jobVisit, error) {
	jobs, err := p.allJobs(r)
	var out []jobVisit
	for _, j := range jobs {
		for _, v := range j.Visits {
			if openVisit(v) {
				out = append(out, jobVisit{Job: j, Visit: v})
			}
		}
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Visit.ScheduledStart.Before(out[k].Visit.ScheduledStart) })
	return out, err
}

type jobsPage struct {
	Open []jobVisit
	Past []gen.Job
}

func (p *Portal) jobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := p.allJobs(r)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	var d jobsPage
	for _, j := range jobs {
		open := false
		for _, v := range j.Visits {
			if openVisit(v) {
				d.Open = append(d.Open, jobVisit{Job: j, Visit: v})
				open = true
			}
		}
		if !open {
			d.Past = append(d.Past, j)
		}
	}
	sort.Slice(d.Open, func(i, k int) bool { return d.Open[i].Visit.ScheduledStart.Before(d.Open[k].Visit.ScheduledStart) })
	p.render(w, r, http.StatusOK, page{name: "jobs", title: "My jobs", nav: "jobs"}, d)
}

type jobPage struct {
	Job     gen.Job
	CanVary bool
	Now     time.Time
}

func (p *Portal) job(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	j, err := call[gen.Job](r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.GetProviderJobsById(w, rq, id) })
	if err != nil {
		p.problem(w, r, err)
		return
	}
	vary := j.Status != nil && (*j.Status == gen.RequestStatusConfirmed || *j.Status == gen.RequestStatusInProgress)
	p.render(w, r, http.StatusOK, page{name: "job", title: j.Service.Name + " " + j.RequestReference, nav: "jobs"}, jobPage{Job: j, CanVary: vary, Now: time.Now()})
}

// eventID keeps the id the page generated, so a double tap or a retried post is applied once.
func eventID(r *http.Request) uuid.UUID {
	if id, err := uuid.Parse(r.PostFormValue("client_event_id")); err == nil {
		return id
	}
	return uuid.New()
}

func backToJob(r *http.Request) string {
	if id, err := uuid.Parse(r.PostFormValue("job")); err == nil {
		return portal + "/jobs/" + id.String()
	}
	return portal + "/jobs"
}

func formFloat(r *http.Request, k string) *float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(r.PostFormValue(k)), 64)
	if err != nil {
		return nil
	}
	return &f
}

// checkIn is the no-JavaScript path (door code). With JavaScript, portal.js queues the same event and
// sends it through sync, which works offline too.
func (p *Portal) checkIn(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	in := gen.CheckInInput{ClientEventId: eventID(r), DeviceRecordedAt: time.Now().UTC(), Method: gen.CheckInInputMethod(r.PostFormValue("method"))}
	if in.Method == "" {
		in.Method = "code"
	}
	if c := strings.TrimSpace(r.PostFormValue("code")); c != "" {
		in.Code = &c
	}
	in.Lat, in.Lng = formFloat(r, "lat"), formFloat(r, "lng")
	if err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderVisitsCheckIn(w, rq, id) }); err != nil {
		p.failed(w, r, backToJob(r), err)
		return
	}
	p.done(w, r, backToJob(r), "Checked in. Check out here when the work is done.")
}

func (p *Portal) checkOut(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	in := gen.CheckOutInput{ClientEventId: eventID(r), DeviceRecordedAt: time.Now().UTC()}
	if n := strings.TrimSpace(r.PostFormValue("notes")); n != "" {
		in.Notes = &n
	}
	in.Lat, in.Lng = formFloat(r, "lat"), formFloat(r, "lng")
	if err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderVisitsCheckOut(w, rq, id) }); err != nil {
		p.failed(w, r, backToJob(r), err)
		return
	}
	p.done(w, r, backToJob(r), "Checked out. The client will be asked to confirm and rate the visit.")
}

// sync receives the check-ins and check-outs queued on the phone (portal.js, or the service worker's
// background sync) and applies them through the same handler as /api/v1/provider/sync.
func (p *Portal) sync(w http.ResponseWriter, r *http.Request) {
	var in gen.SyncBatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<17)).Decode(&in); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"bad_request","message":"Send the saved events as JSON"}`))
		return
	}
	code, _, raw := api.Invoke(r, in, p.api.PostProviderSync)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(raw)
}

func (p *Portal) proposeVariation(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := portal + "/jobs/" + id.String()
	in := gen.VariationCreate{Description: strings.TrimSpace(r.PostFormValue("description"))}
	if q, err := strconv.ParseFloat(strings.TrimSpace(r.PostFormValue("quantity")), 32); err == nil {
		f := float32(q)
		in.Quantity = &f
	}
	if v, err := uuid.Parse(r.PostFormValue("visit_id")); err == nil {
		in.VisitId = &v
	}
	if err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderJobsVariations(w, rq, id) }); err != nil {
		p.failed(w, r, back, err)
		return
	}
	p.done(w, r, back, "Change sent. The client and management will confirm it before you do the extra work.")
}

// ---- offers

func (p *Portal) offers(w http.ResponseWriter, r *http.Request) {
	list, err := call[[]gen.Offer](r, nil, p.api.GetProviderOffers)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	p.render(w, r, http.StatusOK, page{name: "offers", title: "Job offers", nav: "offers"}, list)
}

// offerResult replaces an offer card after an HTMX accept or decline.
type offerResult struct {
	OK      bool
	Message string
	JobID   string
}

func (p *Portal) acceptOffer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	key := r.PostFormValue("idempotency_key")
	params := gen.PostProviderOffersAcceptParams{}
	if key != "" {
		params.IdempotencyKey = &key
	}
	job, err := call[gen.Job](r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderOffersAccept(w, rq, id, params) })
	if web.IsHTMX(r) {
		res := offerResult{OK: err == nil, Message: "Accepted. The job is in My jobs.", JobID: job.AssignmentId.String()}
		if err != nil {
			res.Message = message(err)
		}
		p.fragment(w, r, "offer-result", res)
		return
	}
	if err != nil {
		p.failed(w, r, portal+"/offers", err)
		return
	}
	p.done(w, r, portal+"/jobs/"+job.AssignmentId.String(), "Accepted. Here are the details of your new job.")
}

func (p *Portal) declineOffer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	var in gen.DeclineInput
	if v := strings.TrimSpace(r.PostFormValue("reason")); v != "" {
		in.Reason = &v
	}
	err := do(r, in, func(w http.ResponseWriter, rq *http.Request) { p.api.PostProviderOffersDecline(w, rq, id) })
	if web.IsHTMX(r) {
		res := offerResult{OK: err == nil, Message: "Declined. Thank you for answering quickly."}
		if err != nil {
			res.Message = message(err)
		}
		p.fragment(w, r, "offer-result", res)
		return
	}
	if err != nil {
		p.failed(w, r, portal+"/offers", err)
		return
	}
	p.done(w, r, portal+"/offers", "Offer declined.")
}

// ---- availability and time off

type dayWindow struct {
	Day        int
	Start, End string
}

type availabilityPage struct {
	Days    []dayWindow // Monday first, as people read a week
	TimeOff []gen.TimeOff
}

var weekOrder = []int{1, 2, 3, 4, 5, 6, 0}

func (p *Portal) availability(w http.ResponseWriter, r *http.Request) {
	a, err := call[gen.Availability](r, nil, p.api.GetProviderAvailability)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	off, _ := call[[]gen.TimeOff](r, nil, p.api.GetProviderTimeOff)
	byDay := map[int]dayWindow{}
	for _, x := range a.Weekly {
		if _, seen := byDay[x.DayOfWeek]; !seen { // the page edits one window per day
			byDay[x.DayOfWeek] = dayWindow{Day: x.DayOfWeek, Start: x.StartTime[:5], End: x.EndTime[:5]}
		}
	}
	d := availabilityPage{TimeOff: off}
	for _, day := range weekOrder {
		dw := byDay[day]
		dw.Day = day
		d.Days = append(d.Days, dw)
	}
	p.render(w, r, http.StatusOK, page{name: "availability", title: "When I can work", nav: "more"}, d)
}

func (p *Portal) saveAvailability(w http.ResponseWriter, r *http.Request) {
	var in gen.Availability
	in.Weekly = []struct {
		DayOfWeek int    `json:"day_of_week"`
		EndTime   string `json:"end_time"`
		StartTime string `json:"start_time"`
	}{}
	for _, day := range weekOrder {
		k := strconv.Itoa(day)
		start, end := r.PostFormValue("start_"+k), r.PostFormValue("end_"+k)
		if r.PostFormValue("on_"+k) != "on" || start == "" || end == "" {
			continue
		}
		in.Weekly = append(in.Weekly, struct {
			DayOfWeek int    `json:"day_of_week"`
			EndTime   string `json:"end_time"`
			StartTime string `json:"start_time"`
		}{DayOfWeek: day, StartTime: start, EndTime: end})
	}
	if err := do(r, in, p.api.PutProviderAvailability); err != nil {
		p.failed(w, r, portal+"/availability", err)
		return
	}
	p.done(w, r, portal+"/availability", "Saved. We only offer you jobs in these hours.")
}

// localTime reads <input type="datetime-local">, which is in Kenyan time.
func localTime(r *http.Request, k string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02T15:04", r.PostFormValue(k), eat)
	return t, err == nil
}

func (p *Portal) addTimeOff(w http.ResponseWriter, r *http.Request) {
	start, ok1 := localTime(r, "starts_at")
	end, ok2 := localTime(r, "ends_at")
	if !ok1 || !ok2 {
		p.done(w, r, portal+"/availability", "! Enter when your time off starts and ends.")
		return
	}
	in := gen.TimeOffInput{StartsAt: start, EndsAt: end}
	if v := strings.TrimSpace(r.PostFormValue("reason")); v != "" {
		in.Reason = &v
	}
	if err := do(r, in, p.api.PostProviderTimeOff); err != nil {
		p.failed(w, r, portal+"/availability", err)
		return
	}
	p.done(w, r, portal+"/availability", "Time off added.")
}

func (p *Portal) removeTimeOff(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r, "id")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := do(r, nil, func(w http.ResponseWriter, rq *http.Request) { p.api.DeleteProviderTimeOffById(w, rq, id) }); err != nil {
		p.failed(w, r, portal+"/availability", err)
		return
	}
	p.done(w, r, portal+"/availability", "Time off removed.")
}
