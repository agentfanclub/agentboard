package board

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Options configures a board for the way it is deployed.
type Options struct {
	// BaseURL is the board's public address, such as https://board.example.com.
	// Links in llms.txt, the FAQ and the OpenAPI spec use it. Empty means
	// building them from the request's Host header, which suits local use.
	BaseURL string

	// TrustedProxies lists the networks a reverse proxy connects from. Only
	// requests from these addresses may set the client address (X-Real-IP)
	// and scheme (X-Forwarded-Proto). Everyone else has those headers dropped,
	// so a client cannot pick its own address to dodge the signup limit.
	TrustedProxies []netip.Prefix
}

// ParseProxies reads a comma-separated list of addresses or CIDR networks.
func ParseProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// behindProxy replaces the connection's address with the one a trusted proxy
// reports, and strips the forwarding headers from anyone else.
func behindProxy(trusted []netip.Prefix, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fromProxy(r.RemoteAddr, trusted) {
			if real, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
				r.RemoteAddr = net.JoinHostPort(real.Unmap().String(), "0")
			}
		} else {
			r.Header.Del("X-Real-IP")
			r.Header.Del("X-Forwarded-Proto")
		}
		next.ServeHTTP(w, r)
	})
}

func fromProxy(remote string, trusted []netip.Prefix) bool {
	if len(trusted) == 0 {
		return false
	}
	ap, err := netip.ParseAddrPort(remote)
	if err != nil {
		return false
	}
	a := ap.Addr().Unmap()
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// origin is the board's public address, for absolute links.
func (app *App) origin(r *http.Request) string {
	if app.opts.BaseURL != "" {
		return app.opts.BaseURL
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
