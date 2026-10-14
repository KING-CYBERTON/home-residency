package provider

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/web"
)

// The privacy notice and vetting-checks consent an applicant accepts (Data Protection Act, 2019).
const noticeVersion = "2026-10"

var digits = regexp.MustCompile(`\D`)

// normalisePhone accepts the ways Kenyans write a mobile number (0712…, 712…, 254712…, +254 712 …).
func normalisePhone(in string) string {
	d := digits.ReplaceAllString(in, "")
	switch {
	case strings.HasPrefix(strings.TrimSpace(in), "+"):
		return "+" + d
	case strings.HasPrefix(d, "254"):
		return "+" + d
	case strings.HasPrefix(d, "0") && len(d) == 10:
		return "+254" + d[1:]
	case len(d) == 9:
		return "+254" + d
	}
	return "+" + d
}

type otpForm struct {
	Phone    string
	Name     string
	OtpID    string
	Next     string
	Category string
	Error    string
}

type otpSent struct {
	OtpID string `json:"otp_id"`
}

func safeNext(next string) string {
	if strings.HasPrefix(next, portal+"/") && !strings.Contains(next, "//") && !strings.Contains(next, `\`) {
		return next
	}
	return portal + "/"
}

// ---- sign in

func (p *Portal) loginForm(w http.ResponseWriter, r *http.Request) {
	if pr := auth.FromContext(r.Context()); pr != nil && pr.Kind == auth.KindProvider {
		p.redirect(w, r, portal+"/")
		return
	}
	p.render(w, r, http.StatusOK, page{name: "login", title: "Sign in"}, otpForm{Next: r.URL.Query().Get("next")})
}

func (p *Portal) loginRequest(w http.ResponseWriter, r *http.Request) {
	f := otpForm{Phone: normalisePhone(r.PostFormValue("phone")), Next: r.PostFormValue("next")}
	sent, err := call[otpSent](r, gen.OtpRequest{Phone: f.Phone, Purpose: gen.Login}, p.api.PostAuthOtpRequest)
	if err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "login", title: "Sign in"}, f)
		return
	}
	f.OtpID = sent.OtpID
	p.render(w, r, http.StatusOK, page{name: "login", title: "Enter your code"}, f)
}

func (p *Portal) loginVerify(w http.ResponseWriter, r *http.Request) {
	f := otpForm{Phone: r.PostFormValue("phone"), OtpID: r.PostFormValue("otp_id"), Next: r.PostFormValue("next")}
	id, err := uuid.Parse(f.OtpID)
	if err != nil {
		p.redirect(w, r, portal+"/login")
		return
	}
	kind, err := p.signIn(w, r, gen.OtpVerify{OtpId: id, Code: strings.TrimSpace(r.PostFormValue("code"))})
	if err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "login", title: "Enter your code"}, f)
		return
	}
	if kind != string(gen.UserKindProvider) {
		p.redirect(w, r, web.ClientPortalPath)
		return
	}
	p.redirect(w, r, safeNext(f.Next))
}

// signIn spends the code and passes the session cookie set by the auth handler on to the browser.
func (p *Portal) signIn(w http.ResponseWriter, r *http.Request, in gen.OtpVerify) (string, error) {
	hdr, raw, err := invoke(r, in, p.api.PostAuthOtpVerify)
	if err != nil {
		return "", err
	}
	for _, c := range hdr.Values("Set-Cookie") {
		w.Header().Add("Set-Cookie", c)
	}
	var sess struct {
		User gen.Me `json:"user"`
	}
	_ = json.Unmarshal(raw, &sess)
	return string(sess.User.Kind), nil
}

func (p *Portal) logout(w http.ResponseWriter, r *http.Request) {
	if auth.FromContext(r.Context()) != nil {
		hdr, _, _ := invoke(r, nil, p.api.PostAuthLogout)
		for _, c := range hdr.Values("Set-Cookie") {
			w.Header().Add("Set-Cookie", c)
		}
	}
	// Job pages kept for offline use hold addresses and access details: drop them with the session.
	w.Header().Set("Clear-Site-Data", `"cache"`)
	p.done(w, r, portal+"/login", "You are signed out.")
}

// ---- recruitment

type joinPage struct {
	Categories []gen.Category
}

func (p *Portal) join(w http.ResponseWriter, r *http.Request) {
	cats, err := call[[]gen.Category](r, nil, p.api.GetPublicCategories)
	if err != nil {
		p.problem(w, r, err)
		return
	}
	web.Cacheable(w, 5*time.Minute)
	p.render(w, r, http.StatusOK, page{name: "join", title: "Work with Homend Residence", public: true}, joinPage{Categories: cats})
}

func (p *Portal) applyForm(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("category")
	if pr := auth.FromContext(r.Context()); pr != nil && pr.Kind == auth.KindProvider {
		p.redirect(w, r, portal+"/applications/new?category="+url.QueryEscape(cat))
		return
	}
	p.render(w, r, http.StatusOK, page{name: "apply", title: "Apply to work with us", public: true}, otpForm{Category: cat})
}

func (p *Portal) applyRequest(w http.ResponseWriter, r *http.Request) {
	f := otpForm{Phone: normalisePhone(r.PostFormValue("phone")), Name: strings.TrimSpace(r.PostFormValue("full_name")), Category: r.PostFormValue("category")}
	switch {
	case len(f.Name) < 2:
		f.Error = "Please enter your full name as it appears on your ID."
	case r.PostFormValue("privacy") != "on" || r.PostFormValue("vetting") != "on":
		f.Error = "To apply you need to accept the privacy notice and agree to background checks."
	}
	if f.Error == "" {
		sent, err := call[otpSent](r, gen.OtpRequest{Phone: f.Phone, Purpose: gen.Register}, p.api.PostAuthOtpRequest)
		if err != nil {
			f.Error = message(err)
		}
		f.OtpID = sent.OtpID
	}
	status := http.StatusOK
	if f.Error != "" {
		status = http.StatusUnprocessableEntity
	}
	p.render(w, r, status, page{name: "apply", title: "Apply to work with us", public: true}, f)
}

func (p *Portal) applyVerify(w http.ResponseWriter, r *http.Request) {
	f := otpForm{Phone: r.PostFormValue("phone"), Name: r.PostFormValue("full_name"), OtpID: r.PostFormValue("otp_id"), Category: r.PostFormValue("category")}
	id, err := uuid.Parse(f.OtpID)
	if err != nil {
		p.redirect(w, r, "/join/apply")
		return
	}
	kind := gen.OtpVerifyKindProvider
	consents := []gen.ConsentInput{
		{Purpose: gen.PrivacyNotice, NoticeVersion: noticeVersion, Granted: true},
		{Purpose: gen.VettingChecks, NoticeVersion: noticeVersion, Granted: true},
	}
	if _, err := p.signIn(w, r, gen.OtpVerify{OtpId: id, Code: strings.TrimSpace(r.PostFormValue("code")), FullName: &f.Name, Kind: &kind, Consents: &consents}); err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "apply", title: "Apply to work with us", public: true}, f)
		return
	}
	p.done(w, r, portal+"/applications/new?category="+url.QueryEscape(f.Category), "Welcome! Choose the work you want to apply for.")
}
