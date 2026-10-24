package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

// Staff can see where they are signed in and end a session; an admin can sign a colleague out
// everywhere and give them a new temporary password, which ends their sessions too.
func TestStaffSessionsAndPasswordReset(t *testing.T) {
	h := newHarness(t)
	h.staff("admin", "admin@homend.test", "admin")
	h.staff("finance", "finance@homend.test", "finance")
	pw, secret, _, err := h.app.CreateStaff(context.Background(), "Wanjiru", "wanjiru@homend.test", []string{"triage_officer"})
	if err != nil {
		t.Fatal(err)
	}
	signIn := func(who, password string) {
		t.Helper()
		h.sql(`UPDATE users SET mfa_last_step = NULL WHERE email = 'wanjiru@homend.test'`) // each sign-in here reuses the same 30-second code
		s := h.expect(200, "", "POST", "/auth/staff/login", map[string]any{"email": "wanjiru@homend.test", "password": password}, "User-Agent", "Mozilla/5.0 ("+who+")")
		h.tokens[who] = s["token"].(string)
		code, _ := totp.GenerateCode(secret, time.Now())
		h.expect(200, who, "POST", "/auth/staff/mfa", map[string]any{"code": code})
	}
	signIn("office", pw)
	signIn("laptop", pw)

	mine := h.expectList(200, "office", "GET", "/me/sessions")
	if len(mine) != 2 {
		t.Fatalf("sessions: %v", mine)
	}
	var laptop string
	for _, x := range mine {
		s := x.(map[string]any)
		if s["user_agent"] == "Mozilla/5.0 (laptop)" {
			laptop = s["id"].(string)
			if s["current"] != false {
				t.Fatalf("the laptop is not the session asking: %v", s)
			}
		} else if s["current"] != true || s["mfa_passed"] != true {
			t.Fatalf("this session: %v", s)
		}
	}
	for _, x := range mine {
		if s := x.(map[string]any); s["current"] == true {
			h.expect(409, "office", "DELETE", "/me/sessions/"+s["id"].(string), nil)
		}
	}
	h.expect(204, "office", "DELETE", "/me/sessions/"+laptop, nil)
	h.expect(404, "office", "DELETE", "/me/sessions/"+laptop, nil)
	h.expect(401, "laptop", "GET", "/me", nil)
	if mine = h.expectList(200, "office", "GET", "/me/sessions"); len(mine) != 1 {
		t.Fatalf("after signing the laptop out: %v", mine)
	}
	// someone else's session is not yours to end
	adminSession := h.scan(`SELECT s.id::text FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.email = 'admin@homend.test' AND s.revoked_at IS NULL`)
	h.expect(404, "office", "DELETE", "/me/sessions/"+adminSession, nil)

	id := h.staffID("Wanjiru")
	if list := h.expectList(200, "admin", "GET", "/console/staff/"+id+"/sessions"); len(list) != 1 || list[0].(map[string]any)["current"] != false {
		t.Fatalf("admin view of sessions: %v", list)
	}
	h.expect(403, "finance", "GET", "/console/staff/"+id+"/sessions", nil)
	h.expect(403, "finance", "POST", "/console/staff/"+id+"/sign-out", map[string]any{"reason": "lost phone"})
	h.expect(204, "admin", "POST", "/console/staff/"+id+"/sign-out", map[string]any{"reason": "lost phone"})
	h.expect(401, "office", "GET", "/me", nil)

	signIn("office", pw)
	reset := h.expect(200, "admin", "POST", "/console/staff/"+id+"/reset-password", map[string]any{"reason": "forgot password"})
	fresh, _ := reset["password"].(string)
	if len(fresh) < 12 || fresh == pw {
		t.Fatalf("temporary password: %v", reset)
	}
	h.expect(401, "office", "GET", "/me", nil)
	h.expect(401, "", "POST", "/auth/staff/login", map[string]any{"email": "wanjiru@homend.test", "password": pw})
	signIn("office", fresh) // the authenticator is unchanged
	h.expect(200, "office", "GET", "/me", nil)

	adminID := h.staffID("admin")
	h.expect(409, "admin", "POST", "/console/staff/"+adminID+"/reset-password", map[string]any{"reason": "self"})
	h.expect(404, "admin", "POST", "/console/staff/00000000-0000-0000-0000-000000000000/sign-out", map[string]any{"reason": "nobody"})
	for _, action := range []string{"session.revoke", "staff.sign_out", "staff.reset_password"} {
		if n := h.scan(`SELECT count(*) FROM audit_log WHERE action = $1`, action); n != "1" {
			t.Fatalf("%s audited %s times", action, n)
		}
	}
	if v := h.scan(`SELECT reason FROM audit_log WHERE action = 'staff.reset_password'`); v != "forgot password" {
		t.Fatalf("reason: %s", v)
	}
}
