package dnsconfig

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"
)

const k3sCorefile = `.:53 {
    errors
    health {
       lameduck 5s
    }
    ready
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
        fallthrough in-addr.arpa ip6.arpa
        ttl 30
    }
    hosts /etc/coredns/NodeHosts {
        ttl 60
        reload 15s
        fallthrough
    }
    prometheus :9153
    forward . /etc/resolv.conf {
        max_concurrent 1000
    }
    cache 30 {
       disable success cluster.local
       disable denial cluster.local
    }
    loop
    reload
    loadbalance
}
`

func TestManagerReplacesServiceAliasesWithOptimisticConcurrency(t *testing.T) {
	t.Parallel()

	kube := kubernetesfake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "grafana", Namespace: "monitoring"}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "coredns",
				Namespace:       "kube-system",
				ResourceVersion: "12",
			},
			Data: map[string]string{"Corefile": k3sCorefile},
		},
	)
	manager := New(kube, Options{
		Enabled:       true,
		Namespace:     "kube-system",
		ConfigMapName: "coredns",
		CorefileKey:   "Corefile",
		ClusterDomain: "cluster.local",
	})
	ctx := context.Background()

	initial, err := manager.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !initial.Enabled || !initial.Available || initial.ResourceVersion != "12" {
		t.Fatalf("unexpected initial state: %#v", initial)
	}

	request := ReplaceRequest{
		ResourceVersion: initial.ResourceVersion,
		Aliases: []Alias{{
			Hostname:  "Grafana.Home.Arpa.",
			Service:   "grafana",
			Namespace: "monitoring",
		}},
		DryRun: true,
	}
	preview, err := manager.Replace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun || !strings.Contains(
		preview.Rendered,
		"rewrite stop name exact grafana.home.arpa grafana.monitoring.svc.cluster.local",
	) {
		t.Fatalf("unexpected preview: %#v", preview)
	}
	stored, err := kube.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Data["Corefile"] != k3sCorefile {
		t.Fatalf("dry run changed ConfigMap: %#v", stored.Data)
	}

	request.DryRun = false
	applied, err := manager.Replace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if applied.DryRun || len(applied.Aliases) != 1 ||
		applied.Aliases[0].Hostname != "grafana.home.arpa" ||
		applied.UpdatedAt == nil {
		t.Fatalf("unexpected applied state: %#v", applied)
	}
	stored, err = kube.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Annotations[managedByAnnotation] != "kuchdesk-agent" {
		t.Fatalf("missing manager annotation: %#v", stored.Annotations)
	}
	if !strings.Contains(stored.Data["Corefile"], "forward . /etc/resolv.conf") ||
		!strings.Contains(stored.Data["Corefile"], managedStart) {
		t.Fatalf("existing Corefile content was not preserved: %q", stored.Data["Corefile"])
	}
	request.ResourceVersion = stored.ResourceVersion
	request.Aliases = nil
	if _, err := manager.Replace(ctx, request); err != nil {
		t.Fatal(err)
	}
	stored, err = kube.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err != nil || stored.Data["Corefile"] != k3sCorefile {
		t.Fatalf("removal did not restore Corefile: %q, %v", stored.Data["Corefile"], err)
	}
}

func TestManagerRejectsUnmanagedRewriteAndLeavesCorefileUntouched(t *testing.T) {
	t.Parallel()
	corefile := strings.Replace(k3sCorefile, "    errors", "    rewrite stop name exact api.home.arpa api.apps.svc.cluster.local\n    errors", 1)
	kube := kubernetesfake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "coredns", Namespace: "kube-system", ResourceVersion: "5"},
			Data:       map[string]string{"Corefile": corefile},
		},
	)
	manager := New(kube, Options{Enabled: true, Namespace: "kube-system", ConfigMapName: "coredns", CorefileKey: "Corefile", ClusterDomain: "cluster.local"})
	_, err := manager.Replace(context.Background(), ReplaceRequest{ResourceVersion: "5", Aliases: []Alias{{Hostname: "api.home.arpa", Service: "api", Namespace: "apps"}}})
	if !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("unmanaged rewrite error = %v", err)
	}
	stored, err := kube.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "coredns", metav1.GetOptions{})
	if err != nil || stored.Data["Corefile"] != corefile {
		t.Fatalf("unmanaged Corefile was changed: %v, %v", stored.Data, err)
	}
}

func TestCorefileRequiresSafeKindLayout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		corefile string
		want     error
	}{
		{"missing Corefile", "", ErrUnavailable},
		{"missing reload", strings.Replace(k3sCorefile, "    reload\n", "", 1), ErrUnavailable},
		{"duplicate root", k3sCorefile + k3sCorefile, ErrUnavailable},
		{"incomplete managed block", strings.Replace(k3sCorefile, "    errors", "    "+managedStart+"\n    errors", 1), ErrUnmanaged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := inspectCorefile(tc.corefile, "cluster.local"); !errors.Is(err, tc.want) {
				t.Fatalf("inspectCorefile error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestManagerRejectsUnsafeOrStaleAliases(t *testing.T) {
	t.Parallel()

	kube := kubernetesfake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "coredns",
				Namespace:       "kube-system",
				ResourceVersion: "21",
			},
			Data: map[string]string{"Corefile": k3sCorefile},
		},
	)
	manager := New(kube, Options{
		Enabled:       true,
		Namespace:     "kube-system",
		ConfigMapName: "coredns",
		CorefileKey:   "Corefile",
		ClusterDomain: "cluster.local",
	})
	ctx := context.Background()

	_, err := manager.Replace(ctx, ReplaceRequest{
		ResourceVersion: "21",
		Aliases: []Alias{{
			Hostname:  "api.apps.svc.cluster.local",
			Service:   "api",
			Namespace: "apps",
		}},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("native service alias error = %v", err)
	}

	_, err = manager.Replace(ctx, ReplaceRequest{
		ResourceVersion: "stale",
		Aliases: []Alias{{
			Hostname:  "api.home.arpa",
			Service:   "api",
			Namespace: "apps",
		}},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale resourceVersion error = %v", err)
	}

	_, err = manager.Replace(ctx, ReplaceRequest{
		ResourceVersion: "21",
		Aliases: []Alias{{
			Hostname:  "missing.home.arpa",
			Service:   "missing",
			Namespace: "apps",
		}},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing target error = %v", err)
	}
}

func TestManagerDoesNotOverwriteUnmanagedCorefile(t *testing.T) {
	t.Parallel()

	kube := kubernetesfake.NewSimpleClientset(
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"}},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "coredns",
				Namespace:       "kube-system",
				ResourceVersion: "4",
			},
			Data: map[string]string{
				"Corefile": k3sCorefile + "    " + managedStart + "\n",
			},
		},
	)
	manager := New(kube, Options{
		Enabled:       true,
		Namespace:     "kube-system",
		ConfigMapName: "coredns",
		CorefileKey:   "Corefile",
		ClusterDomain: "cluster.local",
	})

	_, err := manager.Replace(context.Background(), ReplaceRequest{
		ResourceVersion: "4",
		Aliases: []Alias{{
			Hostname:  "api.home.arpa",
			Service:   "api",
			Namespace: "apps",
		}},
	})
	if !errors.Is(err, ErrUnmanaged) {
		t.Fatalf("unmanaged Corefile error = %v", err)
	}
}

func TestManagerReportsMissingConfigMapWithoutCreatingIt(t *testing.T) {
	t.Parallel()

	manager := New(kubernetesfake.NewSimpleClientset(), Options{
		Enabled:       true,
		Namespace:     "kube-system",
		ConfigMapName: "coredns",
		CorefileKey:   "Corefile",
		ClusterDomain: "cluster.local",
	})

	state, err := manager.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Available {
		t.Fatalf("missing ConfigMap reported available: %#v", state)
	}
	_, err = manager.Replace(context.Background(), ReplaceRequest{ResourceVersion: "1"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing ConfigMap replace error = %v", err)
	}
}
