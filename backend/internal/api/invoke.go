package api

import (
	"bytes"
	"encoding/json"
	"net/http"
)

// Invoke runs a handler in-process for the server-rendered portals (ADR-0001: "two thin delivery layers").
// The HTML handler passes its own request so the principal, client IP and user agent carry over, and gets
// back exactly what the JSON route would have answered. No rule is re-implemented outside the handlers.
func Invoke(parent *http.Request, body any, h func(http.ResponseWriter, *http.Request)) (int, http.Header, json.RawMessage) {
	b := []byte("{}")
	if body != nil {
		b, _ = json.Marshal(body)
	}
	// The parent's path keeps idempotency keys scoped to the page action that used them.
	req, _ := http.NewRequestWithContext(parent.Context(), http.MethodPost, parent.URL.Path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = parent.RemoteAddr
	for _, k := range []string{"User-Agent", "X-Forwarded-For", "X-Real-Ip", "X-Request-Id"} {
		if v := parent.Header.Get(k); v != "" {
			req.Header.Set(k, v)
		}
	}
	mw := &memWriter{h: http.Header{}, code: http.StatusOK}
	h(mw, req)
	return mw.code, mw.h, mw.buf.Bytes()
}
