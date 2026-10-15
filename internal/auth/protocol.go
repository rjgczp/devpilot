package auth

import (
	"net/http"

	"github.com/rjgczp/devpilot/internal/config"
)

// CookieName is the single environment selection used by auth and business APIs.
func CookieName(policy config.AuthConfig) string {
	if policy.Mode == "development" {
		return "devpilot_session"
	}
	return "__Host-devpilot_session"
}

// ValidCSRF implements the established same-origin JSON API proof. It does not
// authenticate a user or grant any resource permissions.
func ValidCSRF(r *http.Request, policy config.AuthConfig) bool {
	origins, proof := r.Header.Values("Origin"), r.Header.Values("X-DevPilot-CSRF")
	if len(origins) != 1 || len(proof) != 1 || proof[0] != "1" {
		return false
	}
	origin, err := config.ParseOrigin(origins[0])
	if err != nil || origin != policy.Origin {
		return false
	}
	if sites, exists := r.Header["Sec-Fetch-Site"]; exists && (len(sites) != 1 || sites[0] != "same-origin") {
		return false
	}
	return true
}
