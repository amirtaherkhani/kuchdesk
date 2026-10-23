package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestReconcileDNSUsesConfiguredZoneAndRemainsIdempotent(t *testing.T) {
	cfg, err := parseConfig([]string{"-kube-context", "colima-team", "-zone", "dev.example"})
	if err != nil {
		t.Fatal(err)
	}
	ip := net.ParseIP("10.2.3.4")
	for _, recordExists := range []bool{false, true} {
		t.Run(fmt.Sprintf("record_exists_%t", recordExists), func(t *testing.T) {
			adds := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/zones/list":
					fmt.Fprint(w, `{"status":"ok","response":{"zones":[{"name":"dev.example"}]}}`)
				case "/api/zones/records/get":
					if r.URL.Query().Get("zone") != "dev.example" || r.URL.Query().Get("domain") != "*.dev.example" {
						t.Errorf("unexpected record query: %s", r.URL.RawQuery)
					}
					if recordExists {
						fmt.Fprint(w, `{"status":"ok","response":{"records":[{"name":"*.dev.example","type":"A","ttl":30,"rData":{"ipAddress":"10.2.3.4"}}]}}`)
					} else {
						fmt.Fprint(w, `{"status":"ok","response":{"records":[]}}`)
					}
				case "/api/zones/records/add":
					adds++
					if r.Method != http.MethodPost || r.FormValue("zone") != "dev.example" || r.FormValue("domain") != "*.dev.example" || r.FormValue("ipAddress") != ip.String() {
						t.Errorf("unexpected record write: method=%s form=%v", r.Method, r.Form)
					}
					fmt.Fprint(w, `{"status":"ok"}`)
				default:
					t.Errorf("unexpected API path: %s", r.URL.Path)
					http.Error(w, "unexpected path", http.StatusNotFound)
				}
			}))
			defer server.Close()

			client := &apiClient{base: server.URL, http: &http.Client{Timeout: time.Second}}
			if err := reconcileDNS(context.Background(), client, cfg, ip); err != nil {
				t.Fatal(err)
			}
			wantAdds := 1
			if recordExists {
				wantAdds = 0
			}
			if adds != wantAdds {
				t.Fatalf("record writes = %d, want %d", adds, wantAdds)
			}
		})
	}
}

func TestReconcileUsesConfiguredContextAndMockTechnitium(t *testing.T) {
	var createdZone, addedRecord, loggedOut bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/user/login":
			if r.FormValue("user") != "admin" || r.FormValue("pass") != "test-password" {
				t.Errorf("unexpected login fields")
			}
			fmt.Fprint(w, `{"status":"ok","token":"test-token"}`)
		case "/api/zones/list":
			fmt.Fprint(w, `{"status":"ok","response":{"zones":[]}}`)
		case "/api/zones/create":
			createdZone = r.FormValue("zone") == "dev.example"
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/zones/records/get":
			fmt.Fprint(w, `{"status":"ok","response":{"records":[]}}`)
		case "/api/zones/records/add":
			addedRecord = r.FormValue("domain") == "*.dev.example" && r.FormValue("ipAddress") == "127.0.0.1" && r.FormValue("ttl") == "90" && r.Header.Get("Authorization") == "Bearer test-token"
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/api/user/logout":
			loggedOut = true
			fmt.Fprint(w, `{"status":"ok"}`)
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()
	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "kubectl-mock")
	content := fmt.Sprintf(`#!/bin/sh
case " $* " in
  *" --context colima-team -n technitium port-forward --address 127.0.0.1 svc/technitium :5380 "*)
    printf 'Forwarding from 127.0.0.1:%d -> 5380\n'
    exec sleep 10
    ;;
  *) exit 9 ;;
esac
`, port)
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	root := technitiumValuesFixture(t)
	cfg, err := parseConfig([]string{"-kube-context", "colima-team", "-zone", "dev.example", "-target-ip", "127.0.0.1", "-ttl", "90", "-kubectl", script, "-values-root", root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := reconcile(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if !createdZone || !addedRecord || !loggedOut {
		t.Fatalf("created zone=%t, added record=%t, logged out=%t", createdZone, addedRecord, loggedOut)
	}
}

func TestLocalValuesSupplyCredentialAndKubectlStartsLocalForward(t *testing.T) {
	script := filepath.Join(t.TempDir(), "kubectl-mock")
	content := `#!/bin/sh
case " $* " in
  *" port-forward --address 127.0.0.1 svc/technitium :5380 "*)
    printf '%s\n' 'Forwarding from 127.0.0.1:49123 -> 5380'
    exec sleep 10
    ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	root := technitiumValuesFixture(t)
	cfg, err := parseConfig([]string{"-kube-context", "colima-team", "-zone", "dev.example", "-kubectl", script, "-values-root", root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	password, err := adminPassword(cfg)
	if err != nil || password != "test-password" {
		t.Fatalf("credential = %q, error = %v", password, err)
	}
	port, stop, err := forwardAPI(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if port != "49123" {
		t.Fatalf("forwarded port = %q", port)
	}
}

func technitiumValuesFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "lab/apps/platform/technitium/values.local.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("adminPassword: test-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReconcileDNSUsesConfiguredTTL(t *testing.T) {
	cfg, err := parseConfig([]string{"-kube-context", "colima-team", "-zone", "dev.example", "-ttl", "90"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/zones/list":
			fmt.Fprint(w, `{"status":"ok","response":{"zones":[{"name":"dev.example"}]}}`)
		case "/api/zones/records/get":
			fmt.Fprint(w, `{"status":"ok","response":{"records":[]}}`)
		case "/api/zones/records/add":
			if got := r.FormValue("ttl"); got != "90" {
				t.Errorf("TTL = %q", got)
			}
			fmt.Fprint(w, `{"status":"ok"}`)
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := &apiClient{base: server.URL, http: &http.Client{Timeout: time.Second}}
	if err := reconcileDNS(context.Background(), client, cfg, net.ParseIP("10.2.3.4")); err != nil {
		t.Fatal(err)
	}
}
