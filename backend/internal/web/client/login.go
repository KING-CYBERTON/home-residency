package client

import (
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/KING-CYBERTON/home-residency/backend/internal/api/gen"
	"github.com/KING-CYBERTON/home-residency/backend/internal/auth"
)

// The privacy notice households accept when they register (Data Protection Act 2019).
const privacyNoticeVersion = "2026-10"

func chiParam(r *http.Request, key string) string { return chi.URLParam(r, key) }

type loginForm struct {
	Phone, Name, Email, Mode, Next, OtpID string
	Consent                               bool
	CodeSent                              bool
	Err                                   string
	// UsePhone shows the SMS code form instead of email and password; PhoneOK says SMS codes are
	// available at all.
	UsePhone, PhoneOK bool
}

// newLoginForm reads the fields every sign-in form posts.
func (p *Portal) newLoginForm(r *http.Request, usePhone bool) loginForm {
	f := loginForm{
		Name: strings.TrimSpace(r.FormValue("full_name")), Email: strings.TrimSpace(r.FormValue("email")),
		Mode: r.FormValue("mode"), Next: r.FormValue("next"), Consent: r.FormValue("consent") == "yes", PhoneOK: !p.NoPhoneSignIn,
	}
	f.UsePhone = f.PhoneOK && usePhone
	if f.Mode != "register" {
		f.Mode = "login"
	}
	return f
}

// checkRegistration is what both sign-up forms ask of a new household.
func checkRegistration(f loginForm) string {
	if len(f.Name) < 2 {
		return "Tell us your name so providers know who they are working for."
	}
	if !f.Consent {
		return "Please accept the privacy notice to create an account."
	}
	return ""
}

func (p *Portal) loginPage(w http.ResponseWriter, r *http.Request) {
	if pr := auth.FromContext(r.Context()); pr != nil && pr.Kind == auth.KindClient {
		http.Redirect(w, r, p.Base+"/", http.StatusSeeOther)
		return
	}
	p.render(w, r, "login", &view{Title: "Sign in", Data: p.newLoginForm(r, r.URL.Query().Get("with") == "phone")})
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
	f := p.newLoginForm(r, true)
	f.Phone = phoneE164(r.PostFormValue("phone"))
	if f.Mode == "register" {
		if msg := checkRegistration(f); msg != "" {
			p.loginForm(w, r, f, msg)
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
	f := p.newLoginForm(r, true)
	f.Phone, f.OtpID, f.CodeSent = r.PostFormValue("phone"), r.PostFormValue("otp_id"), true
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
	var sess session
	if err := p.call(r, in, &sess, p.API.PostAuthOtpVerify); err != nil {
		p.loginForm(w, r, f, errText(err))
		return
	}
	p.signedIn(w, r, sess, f)
}

// loginPassword signs in, or creates the account, with email and password.
func (p *Portal) loginPassword(w http.ResponseWriter, r *http.Request) {
	f := p.newLoginForm(r, false)
	var sess session
	var err error
	if f.Mode == "register" {
		if msg := checkRegistration(f); msg != "" {
			p.loginForm(w, r, f, msg)
			return
		}
		in := gen.PasswordRegister{Kind: gen.PasswordRegisterKindClient, FullName: f.Name, Email: openapi_types.Email(f.Email), Password: r.PostFormValue("password"),
			Consents: &[]gen.ConsentInput{{Purpose: "privacy_notice", NoticeVersion: privacyNoticeVersion, Granted: f.Consent}}}
		err = p.call(r, in, &sess, p.API.PostAuthRegister)
	} else {
		err = p.call(r, gen.PasswordLogin{Email: openapi_types.Email(f.Email), Password: r.PostFormValue("password")}, &sess, p.API.PostAuthLogin)
	}
	if err != nil {
		p.loginForm(w, r, f, errText(err))
		return
	}
	p.signedIn(w, r, sess, f)
}

type session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      gen.Me    `json:"user"`
}

// signedIn sets the session cookie and sends the household on: new accounts to add an address.
func (p *Portal) signedIn(w http.ResponseWriter, r *http.Request, sess session, f loginForm) {
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: sess.Token, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: p.CookieSecure, SameSite: http.SameSiteLaxMode})
	if sess.User.Kind != "client" {
		p.redirect(w, r, p.Base+"/")
		return
	}
	// Pages the service worker cached for whoever used this phone before are dropped.
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	next := p.safeNext(f.Next, p.Base+"/")
	if f.Mode == "register" {
		p.flash(w, "Welcome, "+sess.User.FullName+". Add your home address to book your first service.")
		// A household that came from a Book link goes back to that booking once the address is saved.
		addresses := p.Base + "/addresses"
		if next != p.Base+"/" {
			addresses += "?next=" + url.QueryEscape(next)
		}
		next = addresses
	}
	p.redirect(w, r, next)
}

// ---- forgotten password

type resetForm struct {
	Email, Token string
	Sent         bool
}

func (p *Portal) forgotPage(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, "password", &view{Title: "Forgotten password", Data: resetForm{Email: r.URL.Query().Get("email")}})
}

func (p *Portal) forgot(w http.ResponseWriter, r *http.Request) {
	f := resetForm{Email: strings.TrimSpace(r.PostFormValue("email"))}
	if err := p.call(r, gen.PasswordForgot{Email: openapi_types.Email(f.Email)}, nil, p.API.PostAuthPasswordForgot); err != nil {
		p.renderStatus(w, r, "password", http.StatusUnprocessableEntity, &view{Title: "Forgotten password", Error: errText(err), Data: f})
		return
	}
	f.Sent = true
	p.render(w, r, "password", &view{Title: "Check your email", Data: f})
}

func (p *Portal) resetPage(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, "password", &view{Title: "Choose a new password", Data: resetForm{Token: r.URL.Query().Get("token")}})
}

func (p *Portal) reset(w http.ResponseWriter, r *http.Request) {
	f := resetForm{Token: r.PostFormValue("token")}
	if err := p.call(r, gen.PasswordReset{Token: f.Token, Password: r.PostFormValue("password")}, nil, p.API.PostAuthPasswordReset); err != nil {
		p.renderStatus(w, r, "password", http.StatusUnprocessableEntity, &view{Title: "Choose a new password", Error: errText(err), Data: f})
		return
	}
	p.flash(w, "Your password is changed. Sign in with it now.")
	p.redirect(w, r, p.Base+"/login")
}

func (p *Portal) logout(w http.ResponseWriter, r *http.Request) {
	_ = p.call(r, nil, nil, p.API.PostAuthLogout)
	http.SetCookie(w, &http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: p.CookieSecure, SameSite: http.SameSiteLaxMode})
	// "storage" covers the service worker's Cache API copies of address, invoice and profile
	// pages; "cache" alone is only the HTTP cache. The JS purge in client.js is best effort.
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	p.redirect(w, r, p.Base+"/login")
}

// safeNext returns next if it is a path inside the portal, else fallback. Backslashes and
// dot segments are refused because http.Redirect cleans the path, and browsers read
// "/\evil.com" as another host.
func (p *Portal) safeNext(next, fallback string) string {
	if !strings.HasPrefix(next, p.Base+"/") || strings.Contains(next, "//") || strings.Contains(next, `\`) {
		return fallback
	}
	if !strings.HasPrefix(path.Clean(next), p.Base+"/") && path.Clean(next) != p.Base {
		return fallback
	}
	return next
}
