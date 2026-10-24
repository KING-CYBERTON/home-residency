package httpx

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	TrustCDN("s3cret")
	defer TrustCDN("")
	for _, c := range []struct {
		name, xff, verify, want string
	}{
		{"straight to the load balancer", "198.51.100.7", "", "198.51.100.7"},
		{"a forged entry before the real one is ignored", "10.9.9.9, 198.51.100.7", "", "198.51.100.7"},
		{"through CloudFront, the viewer is before CloudFront", "198.51.100.7, 130.176.1.1", "s3cret", "198.51.100.7"},
		{"a wrong secret does not move the hop", "10.9.9.9, 198.51.100.7", "guess", "198.51.100.7"},
		{"no forwarded header", "", "", "192.0.2.1"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "192.0.2.1:5555"
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.verify != "" {
			r.Header.Set("X-Origin-Verify", c.verify)
		}
		if got := ClientIP(r); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
