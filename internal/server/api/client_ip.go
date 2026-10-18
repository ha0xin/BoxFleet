package api

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/haoxin/boxfleet/internal/server/db"
)

type invalidObservationKey struct{}

func ValidateClientIPOptions(source string, cidrs []string) error {
	if source != "" && source != "direct" && source != "cloudflare" {
		return fmt.Errorf("client IP source must be direct or cloudflare")
	}
	if source == "cloudflare" && len(cidrs) == 0 {
		return fmt.Errorf("cloudflare client IP source requires trusted proxy CIDRs")
	}
	for _, cidr := range cidrs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("invalid trusted proxy CIDR %q", cidr)
		}
	}
	return nil
}

// Parsing and peer validation belong to chi. This adapter only selects the
// configured Cloudflare header, including Pseudo IPv4's real IPv6 alternative.
func nodeClientIPMiddleware(options Options) func(http.Handler) http.Handler {
	if err := ValidateClientIPOptions(options.ClientIPSource, options.TrustedProxyCIDRs); err != nil {
		panic(err)
	}
	if options.ClientIPSource != "cloudflare" {
		return middleware.ClientIPFromRemoteAddr
	}
	primary := middleware.ClientIPFromHeader("CF-Connecting-IP", options.TrustedProxyCIDRs...)
	ipv6 := middleware.ClientIPFromHeader("CF-Connecting-IPv6", options.TrustedProxyCIDRs...)
	return func(next http.Handler) http.Handler {
		return primary(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := middleware.GetClientIPAddr(r.Context())
			if ip.Is4() && netip.MustParsePrefix("240.0.0.0/4").Contains(ip) {
				ipv6(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !middleware.GetClientIPAddr(r.Context()).Is6() {
						r = r.WithContext(context.WithValue(r.Context(), invalidObservationKey{}, true))
					}
					next.ServeHTTP(w, r)
				})).ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		}))
	}
}

func observedRequestIP(r *http.Request, source string) *db.ObservedIP {
	if invalid, _ := r.Context().Value(invalidObservationKey{}).(bool); invalid {
		return nil
	}
	ip := middleware.GetClientIPAddr(r.Context())
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.Zone() != "" || netip.MustParsePrefix("240.0.0.0/4").Contains(ip) || netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
		return nil
	}
	if peer, err := netip.ParseAddrPort(r.RemoteAddr); err == nil && peer.Addr().Unmap() == ip {
		source = "direct"
	}
	if source == "" {
		source = "direct"
	}
	return &db.ObservedIP{Address: ip.String(), Source: source}
}
