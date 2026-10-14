package client

import "testing"

func TestSafeNext(t *testing.T) {
	p := &Portal{Deps: Deps{Base: "/app"}}
	for in, want := range map[string]string{
		"/app/invoices/1":    "/app/invoices/1",
		"/app/../\\evil.com": "/app/",
		"/app/..//evil.com":  "/app/",
		"//evil.com":         "/app/",
		"https://evil.com":   "/app/",
		"/app/../admin":      "/app/",
		"/app/x?tab=open":    "/app/x?tab=open",
	} {
		if got := p.safeNext(in, "/app/"); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
