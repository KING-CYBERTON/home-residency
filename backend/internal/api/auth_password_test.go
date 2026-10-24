package api_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/KING-CYBERTON/home-residency/backend/internal/email"
	"github.com/KING-CYBERTON/home-residency/backend/internal/sms"
)

var resetLinkRE = regexp.MustCompile(`https?://\S+/(account|provider)/reset-password\?token=(\S+)`)

// TestEmailPasswordSignIn covers signing up, signing in, locking out, resetting and changing a
// password for households and providers, which is how they sign in when SMS is turned off.
func TestEmailPasswordSignIn(t *testing.T) {
	h := newHarness(t)
	mail := &email.Capture{}
	h.app.Email = mail
	h.app.Cfg.PublicBaseURL = "https://homend.example"
	consents := []any{map[string]any{"purpose": "privacy_notice", "notice_version": "2026-10", "granted": true}}

	// ---- sign up
	h.expect(422, "", "POST", "/auth/register", map[string]any{"kind": "client", "full_name": "Achieng Odhiambo", "email": "achieng@example.com", "password": "short"})
	h.expect(422, "", "POST", "/auth/register", map[string]any{"kind": "client", "full_name": "A", "email": "achieng@example.com", "password": "long enough pw"})
	s := h.expect(200, "", "POST", "/auth/register", map[string]any{"kind": "client", "full_name": "Achieng Odhiambo",
		"email": "Achieng@Example.com", "password": "long enough pw", "consents": consents})
	u := s["user"].(map[string]any)
	if u["kind"] != "client" || u["email"] != "achieng@example.com" || u["has_password"] != true || u["phone"] != nil {
		t.Fatalf("registered user: %v", u)
	}
	h.tokens["achieng"] = s["token"].(string)
	h.expect(200, "achieng", "GET", "/client/addresses", nil)
	if e := h.expect(409, "", "POST", "/auth/register", map[string]any{"kind": "provider", "full_name": "Someone Else",
		"email": "ACHIENG@example.com", "phone": "+254711000999", "password": "another password"}); !strings.Contains(e["message"].(string), "email already exists") {
		t.Fatalf("duplicate email: %v", e)
	}
	if n := h.scan(`SELECT count(*) FROM consents c JOIN users u ON u.id = c.user_id WHERE u.email = 'achieng@example.com'`); n != "1" {
		t.Fatalf("consents recorded: %s", n)
	}

	// ---- sign in
	h.expect(401, "", "POST", "/auth/login", map[string]any{"email": "achieng@example.com", "password": "wrong password"})
	h.expect(401, "", "POST", "/auth/login", map[string]any{"email": "nobody@example.com", "password": "long enough pw"})
	s = h.expect(200, "", "POST", "/auth/login", map[string]any{"email": "ACHIENG@example.com", "password": "long enough pw"})
	if s["mfa_required"] == true || s["user"].(map[string]any)["kind"] != "client" {
		t.Fatalf("login: %v", s)
	}
	// Staff cannot use the household login to skip their authenticator.
	h.staff("admin", "admin@example.com", "admin")
	h.expect(401, "", "POST", "/auth/login", map[string]any{"email": "admin@example.com", "password": "anything at all"})

	// ---- ten wrong passwords lock the account, even for the right one
	for i := 0; i < 10; i++ {
		h.expect(401, "", "POST", "/auth/login", map[string]any{"email": "achieng@example.com", "password": "guess guess"})
	}
	h.expect(429, "", "POST", "/auth/login", map[string]any{"email": "achieng@example.com", "password": "long enough pw"})

	// ---- forgotten password: the same answer for unknown emails, a one-use link for real ones
	h.expect(204, "", "POST", "/auth/password/forgot", map[string]any{"email": "nobody@example.com"})
	h.expect(204, "", "POST", "/auth/password/forgot", map[string]any{"email": "admin@example.com"})
	if len(mail.Msgs) != 0 {
		t.Fatalf("emails sent for unknown or staff addresses: %v", mail.Msgs)
	}
	h.expect(204, "", "POST", "/auth/password/forgot", map[string]any{"email": "achieng@example.com"})
	h.expect(204, "", "POST", "/auth/password/forgot", map[string]any{"email": "achieng@example.com"}) // within the minute: not sent again
	if len(mail.Msgs) != 1 {
		t.Fatalf("want one reset email, got %d", len(mail.Msgs))
	}
	m := resetLinkRE.FindStringSubmatch(mail.Msgs[0].Text)
	if m == nil || mail.Msgs[0].To != "achieng@example.com" || m[1] != "account" || !strings.HasPrefix(m[0], "https://homend.example/account/") {
		t.Fatalf("reset email: %+v", mail.Msgs[0])
	}
	token := m[2]
	h.expect(400, "", "POST", "/auth/password/reset", map[string]any{"token": "not-a-token", "password": "a new password"})
	h.expect(422, "", "POST", "/auth/password/reset", map[string]any{"token": token, "password": "short"})
	h.expect(204, "", "POST", "/auth/password/reset", map[string]any{"token": token, "password": "a new password"})
	h.expect(400, "", "POST", "/auth/password/reset", map[string]any{"token": token, "password": "yet another one"})
	h.expect(401, "achieng", "GET", "/me", nil) // every session was signed out
	h.expect(401, "", "POST", "/auth/login", map[string]any{"email": "achieng@example.com", "password": "long enough pw"})
	s = h.expect(200, "", "POST", "/auth/login", map[string]any{"email": "achieng@example.com", "password": "a new password"}) // lock cleared
	h.tokens["achieng"] = s["token"].(string)
	if v := h.scan(`SELECT email_verified_at IS NOT NULL FROM users WHERE email = 'achieng@example.com'`); v != "true" {
		t.Fatal("a used reset link should confirm the email address")
	}

	// ---- change password while signed in
	h.expect(401, "achieng", "POST", "/me/password", map[string]any{"new_password": "the third password"})
	h.expect(401, "achieng", "POST", "/me/password", map[string]any{"current_password": "wrong", "new_password": "the third password"})
	h.expect(204, "achieng", "POST", "/me/password", map[string]any{"current_password": "a new password", "new_password": "the third password"})
	h.expect(200, "achieng", "GET", "/me", nil) // the session that made the change stays signed in
	h.expect(200, "", "POST", "/auth/login", map[string]any{"email": "achieng@example.com", "password": "the third password"})

	// ---- a phone account adds an email and a password, then signs in with them
	h.register("wanjiru", "+254700000001", "Wanjiru Kamau", "client")
	h.expect(422, "wanjiru", "POST", "/me/password", map[string]any{"new_password": "wanjiru password"})
	h.expect(200, "wanjiru", "PATCH", "/me", map[string]any{"email": "wanjiru@example.com"})
	h.expect(204, "wanjiru", "POST", "/me/password", map[string]any{"new_password": "wanjiru password"})
	if me := h.expect(200, "wanjiru", "GET", "/me", nil); me["has_password"] != true {
		t.Fatalf("me: %v", me)
	}
	h.expect(200, "", "POST", "/auth/login", map[string]any{"email": "wanjiru@example.com", "password": "wanjiru password"})

	// ---- a provider who signed up by email signs the contract with the password
	h.expect(422, "", "POST", "/auth/register", map[string]any{"kind": "provider", "full_name": "Juma Mwangi", "email": "juma@example.com", "password": "juma password"})
	s = h.expect(200, "", "POST", "/auth/register", map[string]any{"kind": "provider", "full_name": "Juma Mwangi", "email": "juma@example.com", "phone": "+254711000888", "password": "juma password"})
	// One phone, one account: a second sign-up with the same number is turned away.
	if e := h.expect(409, "", "POST", "/auth/register", map[string]any{"kind": "provider", "full_name": "Juma Mwangi", "email": "juma2@example.com", "phone": "+254711000888", "password": "juma password"}); !strings.Contains(e["message"].(string), "already uses this phone") {
		t.Fatalf("duplicate phone: %v", e)
	}
	h.tokens["juma"] = s["token"].(string)
	h.sql(`UPDATE providers SET status = 'approved' WHERE user_id = (SELECT id FROM users WHERE email = 'juma@example.com')`)
	offer := h.expect(200, "juma", "GET", "/provider/contract", nil)
	sign := map[string]any{"contract_version": offer["contract_version"], "code_of_conduct_version": offer["code_of_conduct_version"]}
	h.expect(422, "juma", "POST", "/provider/contract/sign", sign)
	sign["password"] = "not my password"
	h.expect(401, "juma", "POST", "/provider/contract/sign", sign)
	sign["password"] = "juma password"
	if signed := h.expect(200, "juma", "POST", "/provider/contract/sign", sign); signed["signed_at"] == nil {
		t.Fatalf("signed: %v", signed)
	}
	if v := h.scan(`SELECT after_state->>'signed_with' FROM audit_log WHERE action = 'contract.sign'`); v != "password" {
		t.Fatalf("audit signed_with = %q", v)
	}
	h.expect(204, "", "POST", "/auth/password/forgot", map[string]any{"email": "juma@example.com"})
	if m := resetLinkRE.FindStringSubmatch(mail.Msgs[len(mail.Msgs)-1].Text); m == nil || m[1] != "provider" {
		t.Fatalf("provider reset link: %+v", mail.Msgs[len(mail.Msgs)-1])
	}
}

// TestSMSTurnedOff: with SMS_PROVIDER=none, phone codes are refused with a pointer to email sign-in.
func TestSMSTurnedOff(t *testing.T) {
	h := newHarness(t)
	h.app.SMS = sms.New("none")
	e := h.expect(503, "", "POST", "/auth/otp/request", map[string]any{"phone": "+254700000001", "purpose": "login"})
	if e["code"] != "sms_unavailable" || !strings.Contains(e["message"].(string), "email") {
		t.Fatalf("otp with sms off: %v", e)
	}
	if n := h.scan(`SELECT count(*) FROM otp_codes`); n != "0" {
		t.Fatalf("no code should be stored, got %s", n)
	}
	h.expect(200, "", "POST", "/auth/register", map[string]any{"kind": "client", "full_name": "Achieng Odhiambo", "email": "achieng@example.com", "password": "long enough pw"})
}

// scan returns the first column of the first row as text.
func (h *harness) scan(q string, args ...any) string {
	h.t.Helper()
	var v any
	if err := h.pool.QueryRow(context.Background(), q, args...).Scan(&v); err != nil {
		h.t.Fatal(err)
	}
	return fmt.Sprint(v)
}
