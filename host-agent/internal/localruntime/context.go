package localruntime

import (
	"errors"
	"regexp"
	"strings"
)

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Colima names Kubernetes contexts from its profile ID.
func Profile(context string) (string, error) {
	if context == "colima" {
		return "default", nil
	}
	if strings.HasPrefix(context, "colima-") && profileName.MatchString(strings.TrimPrefix(context, "colima-")) {
		return strings.TrimPrefix(context, "colima-"), nil
	}
	return "", errors.New("an explicit Colima Kubernetes context is required")
}
