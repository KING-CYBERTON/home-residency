package provider

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
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
	Email    string
	OtpID    string
	Next     string
	Category string
	Error    string
	// UsePhone shows the SMS code form instead of email and password; PhoneOK says SMS codes can
	// be sent at all (false while the service runs with SMS_PROVIDER=none).
	UsePhone bool
	PhoneOK  bool
}

// form starts the sign-in or application form, on the phone tab when ?with=phone asks for it and
// SMS is available.
func (p *Portal) form(r *http.Request) otpForm {
	ok := sms.Enabled(p.api.SMS)
	return otpForm{PhoneOK: ok, UsePhone: ok && r.FormValue("with") == "phone"}
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
	f := p.form(r)
	f.Next = r.URL.Query().Get("next")
	p.render(w, r, http.StatusOK, page{name: "login", title: "Sign in"}, f)
}

func (p *Portal) loginPassword(w http.ResponseWriter, r *http.Request) {
	f := p.form(r)
	f.Email, f.Next = strings.TrimSpace(r.PostFormValue("email")), r.PostFormValue("next")
	kind, err := p.signIn(w, r, gen.PasswordLogin{Email: openapi_types.Email(f.Email), Password: r.PostFormValue("password")}, p.api.PostAuthLogin)
	if err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "login", title: "Sign in"}, f)
		return
	}
	p.afterSignIn(w, r, kind, f.Next)
}

func (p *Portal) loginRequest(w http.ResponseWriter, r *http.Request) {
	f := p.form(r)
	f.UsePhone, f.Phone, f.Next = true, normalisePhone(r.PostFormValue("phone")), r.PostFormValue("next")
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
	f := p.form(r)
	f.UsePhone, f.Phone, f.OtpID, f.Next = true, r.PostFormValue("phone"), r.PostFormValue("otp_id"), r.PostFormValue("next")
	id, err := uuid.Parse(f.OtpID)
	if err != nil {
		p.redirect(w, r, portal+"/login")
		return
	}
	kind, err := p.signIn(w, r, gen.OtpVerify{OtpId: id, Code: strings.TrimSpace(r.PostFormValue("code"))}, p.api.PostAuthOtpVerify)
	if err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "login", title: "Enter your code"}, f)
		return
	}
	p.afterSignIn(w, r, kind, f.Next)
}

func (p *Portal) afterSignIn(w http.ResponseWriter, r *http.Request, kind, next string) {
	if kind != string(gen.UserKindProvider) {
		p.redirect(w, r, web.ClientPortalPath)
		return
	}
	p.redirect(w, r, safeNext(next))
}

// signIn calls an auth handler and passes the session cookie it sets on to the browser.
func (p *Portal) signIn(w http.ResponseWriter, r *http.Request, in any, h http.HandlerFunc) (string, error) {
	hdr, raw, err := invoke(r, in, h)
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
	// The service worker's Cache API and the IndexedDB queue fall under "storage", not "cache";
	// portal.js sends or confirms discarding queued events before the form is submitted.
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
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
	f := p.form(r)
	f.Category = cat
	p.render(w, r, http.StatusOK, page{name: "apply", title: "Apply to work with us", public: true}, f)
}

// applyConsents are the notices an applicant accepts on either form.
func applyConsents() *[]gen.ConsentInput {
	return &[]gen.ConsentInput{
		{Purpose: gen.PrivacyNotice, NoticeVersion: noticeVersion, Granted: true},
		{Purpose: gen.VettingChecks, NoticeVersion: noticeVersion, Granted: true},
	}
}

func checkApplicant(f otpForm, r *http.Request) string {
	switch {
	case len(f.Name) < 2:
		return "Please enter your full name as it appears on your ID."
	case r.PostFormValue("privacy") != "on" || r.PostFormValue("vetting") != "on":
		return "To apply you need to accept the privacy notice and agree to background checks."
	}
	return ""
}

// applyPassword creates the provider account with email and password, signed in straight away.
func (p *Portal) applyPassword(w http.ResponseWriter, r *http.Request) {
	f := p.form(r)
	f.Email, f.Name, f.Category = strings.TrimSpace(r.PostFormValue("email")), strings.TrimSpace(r.PostFormValue("full_name")), r.PostFormValue("category")
	var phone *string
	if raw := strings.TrimSpace(r.PostFormValue("phone")); raw != "" {
		f.Phone = normalisePhone(raw)
		phone = &f.Phone
	}
	if f.Error = checkApplicant(f, r); f.Error == "" {
		in := gen.PasswordRegister{Kind: gen.PasswordRegisterKindProvider, FullName: f.Name, Email: openapi_types.Email(f.Email),
			Phone: phone, Password: r.PostFormValue("password"), Consents: applyConsents()}
		if _, err := p.signIn(w, r, in, p.api.PostAuthRegister); err != nil {
			f.Error = message(err)
		}
	}
	if f.Error != "" {
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "apply", title: "Apply to work with us", public: true}, f)
		return
	}
	p.done(w, r, portal+"/applications/new?category="+url.QueryEscape(f.Category), "Welcome! Choose the work you want to apply for.")
}

func (p *Portal) applyRequest(w http.ResponseWriter, r *http.Request) {
	f := p.form(r)
	f.UsePhone, f.Phone, f.Name, f.Category = true, normalisePhone(r.PostFormValue("phone")), strings.TrimSpace(r.PostFormValue("full_name")), r.PostFormValue("category")
	f.Error = checkApplicant(f, r)
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
	f := p.form(r)
	f.UsePhone, f.Phone, f.Name, f.OtpID, f.Category = true, r.PostFormValue("phone"), r.PostFormValue("full_name"), r.PostFormValue("otp_id"), r.PostFormValue("category")
	id, err := uuid.Parse(f.OtpID)
	if err != nil {
		p.redirect(w, r, "/join/apply")
		return
	}
	kind := gen.OtpVerifyKindProvider
	in := gen.OtpVerify{OtpId: id, Code: strings.TrimSpace(r.PostFormValue("code")), FullName: &f.Name, Kind: &kind, Consents: applyConsents()}
	if _, err := p.signIn(w, r, in, p.api.PostAuthOtpVerify); err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "apply", title: "Apply to work with us", public: true}, f)
		return
	}
	p.done(w, r, portal+"/applications/new?category="+url.QueryEscape(f.Category), "Welcome! Choose the work you want to apply for.")
}

// ---- forgotten password

type resetForm struct {
	Email, Token, Error string
	Sent                bool
}

func (p *Portal) forgotForm(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusOK, page{name: "password", title: "Forgotten password"}, resetForm{Email: r.URL.Query().Get("email")})
}

func (p *Portal) forgot(w http.ResponseWriter, r *http.Request) {
	f := resetForm{Email: strings.TrimSpace(r.PostFormValue("email"))}
	if err := do(r, gen.PasswordForgot{Email: openapi_types.Email(f.Email)}, p.api.PostAuthPasswordForgot); err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "password", title: "Forgotten password"}, f)
		return
	}
	f.Sent = true
	p.render(w, r, http.StatusOK, page{name: "password", title: "Check your email"}, f)
}

func (p *Portal) resetForm(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusOK, page{name: "password", title: "Choose a new password"}, resetForm{Token: r.URL.Query().Get("token")})
}

func (p *Portal) reset(w http.ResponseWriter, r *http.Request) {
	f := resetForm{Token: r.PostFormValue("token")}
	if err := do(r, gen.PasswordReset{Token: f.Token, Password: r.PostFormValue("password")}, p.api.PostAuthPasswordReset); err != nil {
		f.Error = message(err)
		p.render(w, r, http.StatusUnprocessableEntity, page{name: "password", title: "Choose a new password"}, f)
		return
	}
	p.done(w, r, portal+"/login", "Your password is changed. Sign in with it now.")
}
