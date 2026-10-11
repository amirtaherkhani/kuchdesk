package traefiknamespace_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

const wrapper = "../../scripts/helm-traefik.sh"
const renderer = "../../migrations/traefik-platform-networking/post-renderer.py"

func chart(t *testing.T) string {
	t.Helper()
	path := os.Getenv("KUCHDESK_TRAEFIK_CHART_ARCHIVE")
	if path == "" {
		t.Skip("KUCHDESK_TRAEFIK_CHART_ARCHIVE must name the checksum-pinned 39.0.7 archive; see migration README")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != "55a7384ae3a69af13e7dd7f898304a1f9890791e6712774635cd3a2df34da4c8" {
		t.Fatal("Traefik chart archive differs from the reviewed 39.0.7 input")
	}
	return path
}

func objects(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	cmd := exec.Command("yq", "eval-all", "-o=json", "-I=0", "[.]", "-")
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	var docs []map[string]any
	if err := json.Unmarshal(out, &docs); err != nil {
		t.Fatal(err)
	}
	return docs
}

func render(t *testing.T, command string) []map[string]any {
	t.Helper()
	out, err := exec.Command(command, "template", "traefik", chart(t), "-n", "platform-system", "-f", "../../core/ingress/traefik-docker-desktop-values.yaml").CombinedOutput()
	if err != nil {
		t.Fatal("offline render failed:", err, string(out))
	}
	return objects(t, out)
}

func clone(docs []map[string]any) []map[string]any {
	raw, _ := json.Marshal(docs)
	var result []map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}

func input(docs []map[string]any) *bytes.Buffer {
	var result bytes.Buffer
	for i, doc := range docs {
		if i > 0 {
			result.WriteString("\n---\n")
		}
		raw, _ := json.Marshal(doc)
		result.Write(raw)
	}
	return &result
}

func binding(t *testing.T, docs []map[string]any) map[string]any {
	t.Helper()
	for _, doc := range docs {
		if doc["kind"] == "ClusterRoleBinding" {
			return doc
		}
	}
	t.Fatal("missing binding")
	return nil
}

func TestPinnedChartChangesOnlyApprovedNamespaces(t *testing.T) {
	before := render(t, "helm")
	after := render(t, wrapper)
	if len(before) != 6 || len(after) != 6 {
		t.Fatal("reviewed chart must produce exactly six resources")
	}
	want := clone(before)
	moved := 0
	for _, doc := range want {
		metadata := doc["metadata"].(map[string]any)
		switch doc["kind"] {
		case "Deployment", "Service", "ServiceAccount":
			if metadata["name"] != "traefik" || metadata["namespace"] != "platform-system" {
				t.Fatal("unexpected namespaced object", metadata)
			}
			metadata["namespace"] = "platform-networking"
			moved++
		case "ClusterRoleBinding":
			doc["subjects"].([]any)[0].(map[string]any)["namespace"] = "platform-networking"
		}
	}
	// The renderer itself must preserve every field, including null annotations.
	cmd := exec.Command(renderer)
	cmd.Stdin = input(before)
	direct, err := cmd.CombinedOutput()
	if err != nil || !reflect.DeepEqual(want, objects(t, direct)) {
		t.Fatal("renderer changed fields outside the approved namespaces:", err)
	}
	// Helm's post-renderer output handling omits null top-level metadata annotations.
	// Normalize only that known serialization difference in the wrapper comparison.
	for _, docs := range [][]map[string]any{want, after} {
		for _, doc := range docs {
			metadata := doc["metadata"].(map[string]any)
			if metadata["annotations"] == nil {
				delete(metadata, "annotations")
			}
		}
	}
	if moved != 3 || !reflect.DeepEqual(want, after) {
		t.Fatal("wrapper changed something outside the three namespaces and binding subject")
	}
	manual, err := os.ReadFile("../../core/exposure/docker-desktop/traefik-lan.yaml")
	if err != nil {
		t.Fatal(err)
	}
	service := objects(t, manual)[0]
	if service["metadata"].(map[string]any)["namespace"] != "platform-networking" {
		t.Fatal("manual LAN Service would revert the namespace move")
	}
	ports := service["spec"].(map[string]any)["ports"].([]any)
	if len(ports) != 2 || ports[0].(map[string]any)["nodePort"] != float64(32366) || ports[1].(map[string]any)["nodePort"] != float64(32013) {
		t.Fatal("reviewed LAN NodePorts changed")
	}
}

func TestRendererRejectsScopeDriftWithoutOutput(t *testing.T) {
	before := render(t, "helm")
	cases := map[string]func([]map[string]any) []map[string]any{
		"missing":   func(d []map[string]any) []map[string]any { return d[1:] },
		"duplicate": func(d []map[string]any) []map[string]any { return append(d, d[0]) },
		"extra": func(d []map[string]any) []map[string]any {
			return append(d, map[string]any{"kind": "ConfigMap", "metadata": map[string]any{"name": "unexpected"}})
		},
		"source namespace": func(d []map[string]any) []map[string]any {
			for _, doc := range d {
				m := doc["metadata"].(map[string]any)
				if m["namespace"] != nil {
					m["namespace"] = "platform-networking"
				}
			}
			return d
		},
		"cluster namespace": func(d []map[string]any) []map[string]any {
			binding(t, d)["metadata"].(map[string]any)["namespace"] = "platform-system"
			return d
		},
		"role reference": func(d []map[string]any) []map[string]any {
			binding(t, d)["roleRef"].(map[string]any)["name"] = "different-role"
			return d
		},
		"subject": func(d []map[string]any) []map[string]any {
			binding(t, d)["subjects"].([]any)[0].(map[string]any)["name"] = "different-account"
			return d
		},
		"additional subject": func(d []map[string]any) []map[string]any {
			b := binding(t, d)
			b["subjects"] = append(b["subjects"].([]any), map[string]any{"kind": "ServiceAccount", "name": "extra", "namespace": "platform-system"})
			return d
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			docs := mutate(clone(before))
			cmd := exec.Command(renderer)
			cmd.Stdin = input(docs)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "reviewed scope drifted") {
				t.Fatalf("drift did not fail closed: stdout=%q stderr=%q err=%v", stdout.String(), stderr.String(), err)
			}
		})
	}
}

func TestWrapperOwnsRenderer(t *testing.T) {
	for _, arg := range []string{"--post-renderer", "--post-renderer=another"} {
		out, err := exec.Command(wrapper, "template", "traefik", "unused", arg).CombinedOutput()
		if err == nil || !strings.Contains(string(out), "owned by this workflow") {
			t.Fatal("caller renderer accepted:", err, string(out))
		}
	}
	out, err := exec.Command(wrapper, "uninstall", "traefik").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "Usage:") {
		t.Fatal("unsupported command accepted:", err, string(out))
	}
}
