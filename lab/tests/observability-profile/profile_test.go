package observabilityprofile_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func yamlObjects(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	cmd := exec.Command("yq", "eval-all", "-o=json", "-I=0", "[.]", "-")
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatal("yq must parse rendered YAML:", err, string(out))
	}
	var docs []map[string]any
	if err := json.Unmarshal(out, &docs); err != nil {
		t.Fatal(err)
	}
	return docs
}

func field(t *testing.T, object map[string]any, path ...string) any {
	t.Helper()
	var value any = object
	for _, key := range path {
		mapping, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = mapping[key]
	}
	return value
}

func chart(t *testing.T, variable string) string {
	t.Helper()
	path := os.Getenv(variable)
	if path == "" {
		t.Skip(variable + " must name the checksum-pinned chart archive; see README")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func object(t *testing.T, docs []map[string]any, kind, name string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, doc := range docs {
		if doc["kind"] == kind && field(t, doc, "metadata", "name") == name {
			if found != nil {
				t.Fatal("duplicate", kind, name)
			}
			found = doc
		}
	}
	if found == nil {
		t.Fatal("missing", kind, name)
	}
	return found
}

func render(t *testing.T, command string, args ...string) []map[string]any {
	t.Helper()
	out, err := exec.Command(command, args...).CombinedOutput()
	if err != nil {
		t.Fatal("offline render failed:", err, string(out))
	}
	return yamlObjects(t, out)
}

func TestCanonicalProfilesKeepThreeDayRetention(t *testing.T) {
	for _, tc := range []struct {
		name, file, want string
		path             []string
	}{
		{"Loki", "loki", "72h", []string{"loki", "limits_config", "retention_period"}},
		{"Prometheus", "kube-prometheus-stack", "3d", []string{"prometheus", "prometheusSpec", "retention"}},
		{"Alertmanager", "kube-prometheus-stack", "72h", []string{"alertmanager", "alertmanagerSpec", "retention"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := os.ReadFile("../../apps/observability/" + tc.file + "/values.yaml")
			if err != nil {
				t.Fatal(err)
			}
			if got := field(t, yamlObjects(t, raw)[0], tc.path...); got != tc.want {
				t.Fatalf("normal deployment retention = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestLokiRulerReadsSidecarSharedVolume(t *testing.T) {
	docs := render(t, "helm", "template", "loki", chart(t, "KUCHDESK_LOKI_CHART_ARCHIVE"), "-n", "observability", "--kube-version", "1.36.1", "-f", "../../apps/observability/loki/values.yaml")
	cm := object(t, docs, "ConfigMap", "loki")
	var config map[string]any
	for key, value := range field(t, cm, "data").(map[string]any) {
		if strings.HasSuffix(key, "config.yaml") {
			config = yamlObjects(t, []byte(value.(string)))[0]
		}
	}
	directory := field(t, config, "ruler", "storage", "local", "directory")
	sts := object(t, docs, "StatefulSet", "loki")
	containers := field(t, sts, "spec", "template", "spec", "containers").([]any)
	var mainMount, sidecarMount, folder, sidecarImage any
	for _, raw := range containers {
		container := raw.(map[string]any)
		for _, raw := range container["volumeMounts"].([]any) {
			mount := raw.(map[string]any)
			if mount["name"] == "sc-rules-volume" {
				if container["name"] == "loki" {
					mainMount = mount["mountPath"]
				} else if container["name"] == "loki-sc-rules" {
					sidecarMount = mount["mountPath"]
				}
			}
		}
		if container["name"] == "loki-sc-rules" {
			sidecarImage = container["image"]
			for _, raw := range container["env"].([]any) {
				env := raw.(map[string]any)
				if env["name"] == "FOLDER" {
					folder = env["value"]
				}
			}
		}
	}
	if directory == nil || directory != mainMount || mainMount != sidecarMount || sidecarMount != folder {
		t.Fatalf("ruler directory %v, Loki mount %v, sidecar mount %v, watcher folder %v must map to the shared volume", directory, mainMount, sidecarMount, folder)
	}
	if sidecarImage != "docker.io/kiwigrid/k8s-sidecar@sha256:abb55b2165c60e8f97e473862c7a3123df6f67d44f592949577c29b8fb2df9d6" {
		t.Fatal("normal deployment would undo the approved sidecar pin")
	}
}

func TestNormalMonitoringRenderAlignsRetentionRulesAndDashboard(t *testing.T) {
	wrapper, err := filepath.Abs("../../scripts/helm-monitoring.sh")
	if err != nil {
		t.Fatal(err)
	}
	docs := render(t, "bash", wrapper, "template", "monitoring", chart(t, "KUCHDESK_MONITORING_CHART_ARCHIVE"), "-n", "observability", "--kube-version", "1.36.1", "-f", "../../apps/observability/kube-prometheus-stack/values.yaml")
	for _, tc := range []struct{ kind, name, want string }{
		{"Prometheus", "monitoring-kube-prometheus-prometheus", "3d"},
		{"Alertmanager", "monitoring-kube-prometheus-alertmanager", "72h"},
	} {
		if got := field(t, object(t, docs, tc.kind, tc.name), "spec", "retention"); got != tc.want {
			t.Fatalf("%s retention = %v", tc.kind, got)
		}
	}
	rules := object(t, docs, "PrometheusRule", "monitoring-kube-prometheus-kube-apiserver-availability.rules")
	raw, _ := json.Marshal(field(t, rules, "spec", "groups"))
	if bytes.Contains(raw, []byte("30d")) || !bytes.Contains(raw, []byte("3d")) {
		t.Fatal("normal deployment would restore 30-day availability records")
	}
	cm := object(t, docs, "ConfigMap", "monitoring-kube-prometheus-apiserver")
	text := field(t, cm, "data", "apiserver.json").(string)
	if strings.Contains(text, "30d") || strings.Contains(text, "30 days") || !strings.Contains(text, "3d") {
		t.Fatal("API-server dashboard does not match three-day recording rules")
	}
}

func TestNormalMonitoringRenderRejectsMissingRules(t *testing.T) {
	wrapper, err := filepath.Abs("../../scripts/helm-monitoring.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", wrapper, "template", "monitoring", chart(t, "KUCHDESK_MONITORING_CHART_ARCHIVE"), "-n", "observability", "--kube-version", "1.36.1", "-f", "../../apps/observability/kube-prometheus-stack/values.yaml", "--set", "defaultRules.create=false")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil || len(out) != 0 || !strings.Contains(stderr.String(), "post-renderer") {
		t.Fatal("missing availability rules must stop the normal workflow without emitting a candidate", err, stderr.String())
	}
}

func TestMonitoringWorkflowRejectsRendererOverride(t *testing.T) {
	wrapper, err := filepath.Abs("../../scripts/helm-monitoring.sh")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", wrapper, "template", "monitoring", "unused-chart", "--post-renderer=other").CombinedOutput()
	if err == nil || !strings.Contains(string(out), "owned by this workflow") {
		t.Fatal("caller must not bypass the required monitoring renderer", err, string(out))
	}
}
