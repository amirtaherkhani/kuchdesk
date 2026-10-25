package main

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestResolveColimaTargetRefreshesAddressAndAPIPort(t *testing.T) {
	cfg, err := parseConfig([]string{"-kube-context", "colima-team", "-zone", "local.dev", "-target-ip", "auto"})
	if err != nil { t.Fatal(err) }
	for _, test := range []struct{ address, port string }{{"192.168.64.15", "56076"}, {"192.168.64.18", "56123"}} {
		calls := 0
		resolved, err := resolveColimaTarget(context.Background(), cfg, func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls++
			if calls == 1 {
				if name != "colima" || !reflect.DeepEqual(args[:2], []string{"--profile", "team"}) { t.Fatalf("Colima command: %s %v", name, args) }
				return []byte(`{"runtime":"containerd","ip_address":"` + test.address + `"}`), nil
			}
			if name != "kubectl" { t.Fatalf("Kubernetes command: %s", name) }
			return []byte("https://" + test.address + ":" + test.port), nil
		})
		if err != nil || resolved.TargetIP != test.address || resolved.KubeAPIServer != "https://127.0.0.1:" + test.port { t.Fatalf("resolved=%+v err=%v", resolved, err) }
	}
}

func TestResolveColimaTargetRejectsUnavailableOrUnrelatedRuntime(t *testing.T) {
	cfg, _ := parseConfig([]string{"-kube-context", "colima-team", "-zone", "local.dev", "-target-ip", "auto"})
	for _, test := range []struct{ status, endpoint string; failure bool }{
		{failure: true},
		{status: `{"runtime":"docker","ip_address":"192.168.64.15"}`},
		{status: `{"runtime":"containerd","ip_address":"192.168.64.15"}`, endpoint: "https://other.example:6443"},
		{status: `{"runtime":"containerd","ip_address":"192.168.64.15"}`, endpoint: "https://192.168.64.15:99999"},
	} {
		calls := 0
		_, err := resolveColimaTarget(context.Background(), cfg, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
			calls++
			if test.failure { return nil, errors.New("unavailable") }
			if calls == 1 { return []byte(test.status), nil }
			return []byte(test.endpoint), nil
		})
		if err == nil { t.Fatalf("accepted %+v", test) }
	}
}
