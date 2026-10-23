package chartvalues

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"gopkg.in/yaml.v3"
)

// Files are the repository's directly consumed chart values, including the
// private overlays used by fresh local installations.
var Files = map[string][]string{
	"host-bridge":       {"host-agent/bridge-values.local.yaml"},
	"agent":             {"kuchdesk-agent/chart/values.yaml", "kuchdesk-agent/chart/values.local.yaml"},
	"technitium":        {"lab/apps/platform/technitium/values.yaml", "lab/apps/platform/technitium/values.local.yaml"},
	"maildev":           {"lab/apps/platform/maildev/values.yaml"},
	"postgresql":        {"lab/apps/platform/postgresql/values.yaml", "lab/apps/platform/postgresql/auth.local.yaml"},
	"redis":             {"lab/apps/platform/redis/values.yaml", "lab/apps/platform/redis/auth.local.yaml"},
	"rabbitmq":          {"lab/apps/platform/rabbitmq/values.yaml", "lab/apps/platform/rabbitmq/auth.local.yaml"},
	"nats":              {"lab/apps/platform/nats/values.yaml"},
	"cert-manager":      {"lab/apps/platform/cert-manager/values.yaml"},
	"grafana":           {"lab/apps/observability/grafana/values.yaml", "lab/apps/observability/grafana/values.homelab.yaml", "lab/apps/observability/grafana/values.development.yaml", "lab/apps/observability/grafana/values.local.yaml"},
	"kube-prometheus":   {"lab/apps/observability/kube-prometheus-stack/values.yaml"},
	"loki":              {"lab/apps/observability/loki/values.yaml"},
	"tempo":             {"lab/apps/observability/tempo/values.yaml"},
	"alloy":             {"lab/apps/observability/alloy/values.yaml"},
	"alloy-publication": {"lab/apps/observability/alloy-publication/values.yaml"},
	"k6":                {"lab/apps/observability/k6/values.yaml"},
	"traefik":           {"lab/core/ingress/traefik-k3s.yaml"},
	"dns-monitoring":    {"lab/apps/observability/host-dns-monitoring/dns-blackbox.yaml", "lab/apps/observability/host-dns-monitoring/dns-probes.yaml", "lab/apps/observability/host-dns-monitoring/host-scrape.yaml"},
}

var editMu sync.Mutex

func Path(root, service, name string) (string, bool, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", false, errors.New("absolute KuchDesk project root required")
	}
	for _, relative := range Files[service] {
		if filepath.Base(relative) != name {
			continue
		}
		path := filepath.Join(root, relative)
		base, baseErr := filepath.EvalSymlinks(root)
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if baseErr != nil || resolveErr != nil {
			return "", false, errors.New("canonical values file unavailable")
		}
		inside, relErr := filepath.Rel(base, resolved)
		if relErr != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
			return "", false, errors.New("canonical values file outside project root")
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 2<<20 {
			return "", false, errors.New("canonical values file unavailable")
		}
		private := strings.HasSuffix(name, ".local.yaml")
		if private && info.Mode().Perm()&0o077 != 0 {
			return "", false, errors.New("private values file permissions must be 0600")
		}
		return path, private, nil
	}
	return "", false, errors.New("unknown service or values file")
}

func Read(root, service, name string) ([]byte, bool, error) {
	path, private, err := Path(root, service, name)
	if err != nil {
		return nil, false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, errors.New("canonical values file unreadable")
	}
	if len(data) > 2<<20 {
		return nil, false, errors.New("canonical values file exceeds size limit")
	}
	return data, private, nil
}

func Set(root, service, name string, keys []string, valueYAML string) error {
	if len(keys) == 0 || len(keys) > 16 || len(valueYAML) > 1<<16 {
		return errors.New("invalid values edit")
	}
	for _, key := range keys {
		if key == "" || len(key) > 128 || strings.ContainsAny(key, "\r\n\x00") {
			return errors.New("invalid values key")
		}
	}
	editMu.Lock()
	defer editMu.Unlock()
	path, private, err := Path(root, service, name)
	if err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(filepath.Dir(path), ".kuchdesk-values.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.New("canonical YAML lock unavailable")
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("canonical YAML is busy or locking unavailable")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if !private {
		leaf := strings.ToLower(keys[len(keys)-1])
		if strings.Contains(leaf, "password") || leaf == "token" || strings.Contains(leaf, "clientsecret") || strings.Contains(leaf, "privatekey") {
			return errors.New("credential values belong in private local YAML")
		}
	}
	data, _, err := Read(root, service, name)
	if err != nil {
		return err
	}
	documents, err := decodeDocuments(data)
	if err != nil {
		return errors.New("invalid canonical YAML")
	}
	var replacement yaml.Node
	if err := decodeOne([]byte(valueYAML), &replacement); err != nil || replacement.Content[0].Kind == yaml.AliasNode {
		return errors.New("invalid replacement YAML")
	}
	documentIndex := 0
	if len(documents) > 1 {
		index, err := strconv.Atoi(keys[0])
		if err != nil || index < 0 || index >= len(documents) || len(keys) == 1 {
			return errors.New("multi-document YAML requires a document index and key")
		}
		documentIndex = index
		keys = keys[1:]
	}
	node := documents[documentIndex].Content[0]
	for _, key := range keys {
		switch node.Kind {
		case yaml.MappingNode:
			found := false
			for index := 0; index < len(node.Content); index += 2 {
				if node.Content[index].Value == key {
					node = node.Content[index+1]
					found = true
					break
				}
			}
			if !found {
				return errors.New("values key does not exist")
			}
		case yaml.SequenceNode:
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(node.Content) {
				return errors.New("values index does not exist")
			}
			node = node.Content[index]
		default:
			return errors.New("values path is not a mapping or sequence")
		}
	}
	oldHead, oldLine := node.HeadComment, node.LineComment
	*node = *replacement.Content[0]
	node.HeadComment, node.LineComment = oldHead, oldLine
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	for index := range documents {
		if err := encoder.Encode(&documents[index]); err != nil {
			return errors.New("cannot encode canonical YAML")
		}
	}
	if err := encoder.Close(); err != nil {
		return errors.New("cannot close YAML encoder")
	}
	mode := os.FileMode(0o644)
	if private {
		mode = 0o600
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".values-edit-*")
	if err != nil {
		return errors.New("cannot prepare values edit")
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return errors.New("cannot set values permissions")
	}
	if _, err := temp.Write(output.Bytes()); err != nil {
		temp.Close()
		return errors.New("cannot write values")
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return errors.New("cannot sync values")
	}
	if err := temp.Close(); err != nil {
		return errors.New("cannot close values")
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return errors.New("cannot replace values")
	}
	return nil
}

func decodeOne(data []byte, document *yaml.Node) error {
	documents, err := decodeDocuments(data)
	if err != nil || len(documents) != 1 {
		return errors.New("expected one YAML document")
	}
	*document = documents[0]
	return nil
}

func decodeDocuments(data []byte) ([]yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var documents []yaml.Node
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil || len(document.Content) != 1 || len(documents) >= 64 {
			return nil, errors.New("invalid YAML document stream")
		}
		documents = append(documents, document)
	}
	if len(documents) == 0 {
		return nil, errors.New("empty YAML document stream")
	}
	return documents, nil
}

// Inspect returns YAML with all private scalar values hidden. Public values
// remain visible except for credential-named keys.
func Inspect(root, service, name string) (string, error) {
	data, private, err := Read(root, service, name)
	if err != nil {
		return "", err
	}
	documents, err := decodeDocuments(data)
	if err != nil {
		return "", errors.New("invalid canonical YAML")
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	for index := range documents {
		documents[index].HeadComment, documents[index].LineComment, documents[index].FootComment = "", "", ""
		hide(documents[index].Content[0], private)
		if err := encoder.Encode(&documents[index]); err != nil {
			return "", errors.New("cannot inspect canonical YAML")
		}
	}
	return output.String(), nil
}

func hide(node *yaml.Node, private bool) {
	node.HeadComment, node.LineComment, node.FootComment = "", "", ""
	if node.Kind == yaml.MappingNode {
		for index := 0; index < len(node.Content); index += 2 {
			node.Content[index].HeadComment, node.Content[index].LineComment, node.Content[index].FootComment = "", "", ""
			key := strings.ToLower(node.Content[index].Value)
			sensitive := private || strings.Contains(key, "password") || strings.Contains(key, "token") || strings.Contains(key, "credential") || strings.Contains(key, "privatekey")
			hide(node.Content[index+1], sensitive)
		}
		return
	}
	if node.Kind == yaml.SequenceNode {
		for _, child := range node.Content {
			hide(child, private)
		}
		return
	}
	if private {
		node.Kind, node.Tag, node.Value, node.Content = yaml.ScalarNode, "!!str", "[redacted]", nil
	}
}
