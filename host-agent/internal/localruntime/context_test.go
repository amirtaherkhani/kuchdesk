package localruntime

import "testing"

func TestOnlyExplicitColimaContexts(t *testing.T) {
	for _, item := range []struct{ context, profile string }{{"colima", "default"}, {"colima-kuchdesk-fresh", "kuchdesk-fresh"}} {
		got, err := Profile(item.context)
		if err != nil || got != item.profile {
			t.Fatal("Colima context was not resolved")
		}
	}
	for _, name := range []string{"", "docker-desktop", "kind-team", "colima-", "colima-../other"} {
		if _, err := Profile(name); err == nil {
			t.Fatal("unsupported context accepted")
		}
	}
}
