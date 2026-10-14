package client

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
)

// The privacy notice households accept when they register (Data Protection Act 2019).
const privacyNoticeVersion = "2026-10"

func chiParam(r *http.Request, key string) string { return chi.URLParam(r, key) }

type loginForm struct {
	Phone, Name, Mode, Next, OtpID string
	Consent                        bool
	CodeSent                       bool
	Err                            string
}

func (p *Portal) loginPage(w http.ResponseWriter, r *http.Request) {
	if pr := auth.FromContext(r.Context()); pr != nil && pr.Kind == auth.KindClient {
		http.Redirect(w, r, p.Base+"/", http.StatusSeeOther)
		return
	}
	mode := "login"
	if r.URL.Query().Get("mode") == "register" {
		mode = "register"
	}
	p.render(w, r, "login", &view{Title: "Sign in", Data: loginForm{Mode: mode, Next: r.URL.Query().Get("next")}})
}

func (p *Portal) loginForm(w http.ResponseWriter, r *http.Request, f loginForm, errMsg string) {
	f.Err = errMsg
	v := &view{Title: "Sign in", Data: f}
	if isHTMX(r) {
		p.renderBlock(w, r, "login", "login-form", v)
		return
	}
	p.render(w, r, "login", v)
}

// loginStart sends the SMS code, for signing in or for creating an account.
func (p *Portal) loginStart(w http.ResponseWriter, r *http.Request) {
	f := loginForm{
		Phone: phoneE164(r.PostFormValue("phone")), Name: strings.TrimSpace(r.PostFormValue("full_name")),
		Mode: r.PostFormValue("mode"), Next: r.PostFormValue("next"), Consent: r.PostFormValue("consent") == "yes",
	}
	if f.Mode != "register" {
		f.Mode = "login"
	}
	if f.Mode == "register" {
		if len(f.Name) < 2 {
			p.loginForm(w, r, f, "Tell us your name so providers know who they are working for.")
			return
		}
		if !f.Consent {
			p.loginForm(w, r, f, "Please accept the privacy notice to create an account.")
			return
		}
	}
	var res gen.OtpRequestResult
	purpose := gen.OtpRequestPurpose(f.Mode)
	if err := p.call(r, gen.OtpRequest{Phone: f.Phone, Purpose: purpose}, &res, p.API.PostAuthOtpRequest); err != nil {
		p.loginForm(w, r, f, errText(err))
		return
	}
	if res.OtpId != nil {
		f.OtpID = res.OtpId.String()
	}
	f.CodeSent = true
	p.loginForm(w, r, f, "")
}

func (p *Portal) loginVerify(w http.ResponseWriter, r *http.Request) {
	f := loginForm{
		Phone: r.PostFormValue("phone"), Name: strings.TrimSpace(r.PostFormValue("full_name")), Mode: r.PostFormValue("mode"),
		Next: r.PostFormValue("next"), OtpID: r.PostFormValue("otp_id"), Consent: r.PostFormValue("consent") == "yes", CodeSent: true,
	}
	otpID, ok := formUUID(r, "otp_id")
	if !ok {
		f.CodeSent = false
		p.loginForm(w, r, f, "That code has expired. Ask for a new one.")
		return
	}
	in := gen.OtpVerify{Code: strings.TrimSpace(r.PostFormValue("code")), OtpId: otpID}
	if f.Mode == "register" {
		kind := gen.OtpVerifyKind("client")
		in.Kind, in.FullName = &kind, &f.Name
		in.Consents = &[]gen.ConsentInput{{Purpose: "privacy_notice", NoticeVersion: privacyNoticeVersion, Granted: f.Consent}}
	}
	var sess struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
		User      gen.Me    `json:"user"`
	}
	if err := p.call(r, in, &sess, p.API.PostAuthOtpVerify); err != nil {
		p.loginForm(w, r, f, errText(err))
		return
	}
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: sess.Token, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: p.CookieSecure, SameSite: http.SameSiteLaxMode})
	if sess.User.Kind != "client" {
		p.redirect(w, r, p.Base+"/")
		return
	}
	next := p.Base + "/"
	if strings.HasPrefix(f.Next, p.Base+"/") && !strings.HasPrefix(f.Next, "//") {
		next = f.Next
	}
	if f.Mode == "register" {
		p.flash(w, "Welcome, "+sess.User.FullName+". Add your home address to book your first service.")
		next = p.Base + "/addresses"
	}
	p.redirect(w, r, next)
}

func (p *Portal) logout(w http.ResponseWriter, r *http.Request) {
	_ = p.call(r, nil, nil, p.API.PostAuthLogout)
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: p.CookieSecure, SameSite: http.SameSiteLaxMode})
	p.redirect(w, r, p.Base+"/login")
}
