package infisical

import (
	"errors"
	"net/http"
	"testing"
)

func TestPublicErrorClassifiesWithoutLeakingDetails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"policy denial", ErrPolicyDenied, "policy_denied"},
		{"policy read failure", ErrPolicyUnavailable, "policy_unavailable"},
		{"context missing", ErrContextUnavailable, "policy_unavailable"},
		{"identity listing failure", ErrIdentityUnavailable, "identity_projects_unavailable"},
		{"schema", ErrInvalidResponse, "upstream_schema_error"},
		{"authentication", &APIError{StatusCode: http.StatusUnauthorized}, "authentication_rejected"},
		{"permission", &APIError{StatusCode: http.StatusForbidden}, "permission_denied"},
		{"missing", &APIError{StatusCode: http.StatusNotFound}, "not_found"},
		{"request", &APIError{StatusCode: http.StatusBadRequest}, "upstream_request_rejected"},
		{"transport", errors.New("sensitive target and token"), "infisical_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PublicError(tc.err).Error(); got != tc.want {
				t.Fatalf("public error = %q, want %q", got, tc.want)
			}
		})
	}
}
