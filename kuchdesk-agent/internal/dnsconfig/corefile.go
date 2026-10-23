package dnsconfig

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/coredns/caddy/caddyfile"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	managedStart     = "# BEGIN KuchDesk service aliases"
	managedEnd       = "# END KuchDesk service aliases"
	maxCorefileBytes = 1 << 20
)

type corefileLayout struct {
	insertAt   int
	blockStart int
	blockEnd   int
	rendered   string
	aliases    []Alias
}

// inspectCorefile accepts the standard Kubernetes CoreDNS server block and refuses
// ambiguous or incomplete managed sections before any Kubernetes write.
func inspectCorefile(corefile, clusterDomain string) (corefileLayout, error) {
	if corefile == "" || len(corefile) > maxCorefileBytes {
		return corefileLayout{}, fmt.Errorf("%w: Corefile is missing or too large", ErrUnavailable)
	}
	if _, err := caddyfile.Parse("Corefile", strings.NewReader(corefile), nil); err != nil {
		return corefileLayout{}, fmt.Errorf("%w: invalid Corefile: %v", ErrUnavailable, err)
	}

	layout := corefileLayout{blockStart: -1, blockEnd: -1}
	depth, offset := 0, 0
	rootFound, inRoot, inManaged := false, false, false
	hasKubernetes, hasReload := false, false
	contentStart, contentEnd := -1, -1
	for _, line := range strings.SplitAfter(corefile, "\n") {
		trimmed := strings.TrimSpace(line)
		if depth == 0 && trimmed == ".:53 {" {
			if rootFound {
				return corefileLayout{}, fmt.Errorf("%w: multiple .:53 server blocks", ErrUnavailable)
			}
			rootFound, inRoot = true, true
			layout.insertAt = offset + len(line)
		}
		if trimmed == managedStart || trimmed == managedEnd {
			if !inRoot || depth != 1 {
				return corefileLayout{}, fmt.Errorf("%w: managed marker is outside the .:53 server block", ErrUnmanaged)
			}
			if trimmed == managedStart {
				if layout.blockStart >= 0 {
					return corefileLayout{}, fmt.Errorf("%w: duplicate start marker", ErrUnmanaged)
				}
				layout.blockStart = offset
				contentStart = offset + len(line)
				inManaged = true
			} else {
				if layout.blockStart < 0 || layout.blockEnd >= 0 {
					return corefileLayout{}, fmt.Errorf("%w: unmatched end marker", ErrUnmanaged)
				}
				contentEnd = offset
				layout.blockEnd = offset + len(line)
				inManaged = false
			}
		}
		if inRoot && depth == 1 {
			fields := strings.Fields(strings.SplitN(trimmed, "#", 2)[0])
			if len(fields) > 0 {
				if fields[0] == "rewrite" && !inManaged {
					return corefileLayout{}, fmt.Errorf("%w: existing rewrite outside managed block", ErrUnmanaged)
				}
				hasKubernetes = hasKubernetes || fields[0] == "kubernetes"
				hasReload = hasReload || fields[0] == "reload"
			}
		}
		depth += corefileBraceDelta(line)
		if depth < 0 {
			return corefileLayout{}, fmt.Errorf("%w: unmatched Corefile brace", ErrUnavailable)
		}
		if inRoot && depth == 0 {
			inRoot = false
		}
		offset += len(line)
	}
	if !rootFound || depth != 0 || !hasKubernetes || !hasReload {
		return corefileLayout{}, fmt.Errorf("%w: .:53 server block with kubernetes and reload is required", ErrUnavailable)
	}
	if (layout.blockStart < 0) != (layout.blockEnd < 0) {
		return corefileLayout{}, fmt.Errorf("%w: incomplete managed block", ErrUnmanaged)
	}
	if layout.blockStart >= 0 {
		aliases, err := parseManaged(corefile[contentStart:contentEnd], clusterDomain)
		if err != nil {
			return corefileLayout{}, err
		}
		layout.aliases = aliases
		layout.rendered = render(aliases, clusterDomain)
	} else {
		layout.aliases = []Alias{}
	}
	return layout, nil
}

func updateManagedCorefile(corefile, rendered, clusterDomain string) (string, error) {
	layout, err := inspectCorefile(corefile, clusterDomain)
	if err != nil {
		return "", err
	}
	newline := "\n"
	if strings.Contains(corefile, "\r\n") {
		newline = "\r\n"
	}
	block := ""
	if rendered != "" {
		block = "    " + managedStart + newline
		for _, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
			block += "    " + line + newline
		}
		block += "    " + managedEnd + newline
	}
	if layout.blockStart >= 0 {
		corefile = corefile[:layout.blockStart] + block + corefile[layout.blockEnd:]
	} else if block != "" {
		corefile = corefile[:layout.insertAt] + block + corefile[layout.insertAt:]
	}
	if _, err := inspectCorefile(corefile, clusterDomain); err != nil {
		return "", fmt.Errorf("validate updated Corefile: %w", err)
	}
	return corefile, nil
}

func parseManaged(input, clusterDomain string) ([]Alias, error) {
	if len(input) > maxManagedBytes {
		return nil, fmt.Errorf("%w: managed block is larger than %d bytes", ErrUnmanaged, maxManagedBytes)
	}
	aliases := make([]Alias, 0)
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 6 || fields[0] != "rewrite" || fields[1] != "stop" || fields[2] != "name" || fields[3] != "exact" {
			return nil, fmt.Errorf("%w: unexpected directive %q", ErrUnmanaged, line)
		}
		targetParts := strings.Split(fields[5], ".")
		if len(targetParts) < 5 || targetParts[2] != "svc" || strings.Join(targetParts[3:], ".") != clusterDomain ||
			len(validation.IsDNS1123Label(targetParts[0])) > 0 || len(validation.IsDNS1123Label(targetParts[1])) > 0 ||
			len(validation.IsDNS1123Subdomain(fields[4])) > 0 || seen[fields[4]] {
			return nil, fmt.Errorf("%w: unexpected alias %q", ErrUnmanaged, line)
		}
		seen[fields[4]] = true
		aliases = append(aliases, Alias{Hostname: fields[4], Service: targetParts[0], Namespace: targetParts[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: read managed block: %v", ErrUnmanaged, err)
	}
	return aliases, nil
}

func validateAliasDirectives(rendered string) error {
	if rendered == "" {
		return nil
	}
	_, err := caddyfile.Parse("KuchDesk aliases", strings.NewReader(".:53 {\n"+rendered+"}\n"), []string{"rewrite"})
	return err
}

// Corefile syntax has already been parsed; this finds the root block boundary
// while ignoring braces in comments and quoted arguments. Unknown layouts fail closed.
func corefileBraceDelta(line string) int {
	delta := 0
	var quote rune
	escaped := false
	for _, char := range line {
		if escaped {
			escaped = false
			continue
		}
		if char == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '#' {
			break
		}
		if char == '"' || char == '\'' {
			quote = char
			continue
		}
		if char == '{' {
			delta++
		} else if char == '}' {
			delta--
		}
	}
	return delta
}
