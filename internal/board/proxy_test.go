package board

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnlyTrustedProxiesSetTheClientAddress(t *testing.T) {
	trusted, err := ParseProxies("172.16.0.0/12, 127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	var gotIP string
	var gotHTTPS bool
	h := behindProxy(trusted, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIP, gotHTTPS = clientIP(r), isHTTPS(r)
	}))

	tests := []struct {
		name, remote string
		wantIP       string
		wantHTTPS    bool
	}{
		{"from the proxy", "172.18.0.1:4242", "203.0.113.9", true},
		{"from loopback", "127.0.0.1:4242", "203.0.113.9", true},
		{"straight from a client", "198.51.100.7:4242", "198.51.100.7", false},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		r.Header.Set("X-Real-IP", "203.0.113.9")
		r.Header.Set("X-Forwarded-Proto", "https")
		h.ServeHTTP(httptest.NewRecorder(), r)
		if gotIP != tt.wantIP || gotHTTPS != tt.wantHTTPS {
			t.Errorf("%s: ip %s https %v, want %s %v", tt.name, gotIP, gotHTTPS, tt.wantIP, tt.wantHTTPS)
		}
	}

	if _, err := ParseProxies("10.0.0.0/8,nonsense"); err == nil {
		t.Error("a bad proxy address should be an error")
	}
}

func TestBaseURLFixesAbsoluteLinks(t *testing.T) {
	app, _ := newTestApp(t)
	app.opts.BaseURL = "https://board.example.com"
	r := httptest.NewRequest("GET", "/llms.txt", nil)
	r.Host = "evil.example"
	if got := app.origin(r); got != "https://board.example.com" {
		t.Errorf("origin = %s", got)
	}
}
