package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const buildFingerprintLabel = "io.kuchdesk.build-fingerprint.v1"

// Serialize checks with the corresponding mutation. Entries are removed when
// idle. This coordinates this process, not independent CLI/daemon processes.
var imageLocks = struct {
	sync.Mutex
	entries map[string]*imageLock
}{entries: map[string]*imageLock{}}

type imageLock struct {
	token chan struct{}
	users int
}

func lockImage(ctx context.Context, key string) (func(), error) {
	imageLocks.Lock()
	entry := imageLocks.entries[key]
	if entry == nil {
		entry = &imageLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		imageLocks.entries[key] = entry
	}
	entry.users++
	imageLocks.Unlock()
	drop := func() {
		imageLocks.Lock()
		entry.users--
		if entry.users == 0 {
			delete(imageLocks.entries, key)
		}
		imageLocks.Unlock()
	}
	select {
	case <-ctx.Done():
		drop()
		return nil, ctx.Err()
	case <-entry.token:
		return func() { entry.token <- struct{}{}; drop() }, nil
	}
}

func (w Workflow) buildImage(ctx context.Context, s Spec, image string) error {
	unlock, err := lockImage(ctx, "docker-build/"+image)
	if err != nil {
		return err
	}
	defer unlock()
	fingerprint, reason, err := buildFingerprint(s)
	if err != nil {
		return err
	}
	builder, store := "", ""
	if fingerprint != "" {
		evidence, selectedBuilder, selectedStore, evidenceReason := w.buildDependencyEvidence(ctx, s)
		builder, store = selectedBuilder, selectedStore
		if evidenceReason != "" {
			fingerprint = ""
			reason = evidenceReason
		} else {
			sum := sha256.Sum256([]byte(fingerprint + "\x00" + evidence))
			fingerprint = hex.EncodeToString(sum[:])
		}
	}
	matches, exists, err := w.localBuildMatches(ctx, s, image, fingerprint, store)
	if err != nil {
		return err
	}
	if exists {
		if fingerprint != "" && matches && !s.Rebuild && !s.RefreshImages {
			w.progress("reuse matching Docker build")
			return nil
		}
		return fmt.Errorf("immutable image tag %s already exists; reuse its exact verified artifact or commit changed inputs and use the resulting source tag", image)
	}
	if s.ReuseBuild && reason != "" {
		w.progress("build reuse disabled: " + reason)
	}
	args := []string{"build", "--pull", "--file", s.Dockerfile, "--tag", image}
	if s.Platform != "" {
		args = append(args, "--platform", s.Platform)
	}
	if s.Rebuild || s.RefreshImages {
		args = append(args, "--no-cache")
	}
	if fingerprint != "" {
		args = append(args, "--builder", builder, "--network", "none", "--label", buildFingerprintLabel+"="+fingerprint)
	}
	buildArgs := s.BuildArgs
	if fingerprint != "" {
		buildArgs = offlineBuildArgs(s.BuildArgs)
	}
	for _, key := range sortedBuildArgs(buildArgs) {
		args = append(args, "--build-arg", key+"="+buildArgs[key])
	}
	if err := w.run(ctx, s.SourceDir, "docker build", "docker", append(args, s.SourceDir)...); err != nil {
		return err
	}
	if fingerprint == "" {
		return nil
	}
	local, exists, err := w.inspectLocalBuild(ctx, s, image)
	if err != nil || !exists || !local.matches(s.Platform, fingerprint) {
		return fmt.Errorf("built image identity verification failed for %s", image)
	}
	return w.writeBuildReceipt(buildReceipt{Version: 1, Store: store, Image: image, Platform: s.Platform, Fingerprint: fingerprint, ImageID: local.ID})
}

func sortedBuildArgs(args map[string]string) []string {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Reuse is deliberately opt-in and limited to local, offline inputs. The full
// context (including ignored files) is hashed, making omissions conservative.
// Mutable FROM/frontends, remote ADD/COPY, mounts and symlinks disable reuse.
func buildFingerprint(s Spec) (string, string, error) {
	if !s.ReuseBuild {
		return "", "", nil
	}
	if s.Platform == "" {
		return "", "explicit platform required", nil
	}
	if _, custom := s.BuildArgs["BUILDKIT_SYNTAX"]; custom {
		return "", "explicit custom frontend argument", nil
	}
	if os.Getenv("DOCKER_HOST") != "" {
		return "", "implicit Docker endpoint is not eligible for reuse", nil
	}
	if os.Getenv("DOCKER_BUILDKIT") == "0" {
		return "", "offline reuse requires the default BuildKit builder", nil
	}
	data, err := os.ReadFile(s.Dockerfile)
	if err != nil {
		return "", "", fmt.Errorf("build Dockerfile unavailable: %w", err)
	}
	if !offlineDockerfile(string(data)) {
		return "", "unresolved or external dependency inputs", nil
	}
	hash := sha256.New()
	metadata, _ := json.Marshal(struct {
		Version, Dockerfile, Platform, Network string
		Args                                   map[string]string
		BuildEnvironment                       []string
	}{"v2", s.Dockerfile, s.Platform, "none", offlineBuildArgs(s.BuildArgs), buildEnvironment()})
	hash.Write(metadata)
	unsupported := false
	err = filepath.WalkDir(s.SourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(s.SourceDir, path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			unsupported = true
			return nil
		}
		fmt.Fprintf(hash, "\x00%s\x00%d\x00%d\x00", filepath.ToSlash(relative), info.Mode(), info.Size())
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		}
		return nil
	})
	if err != nil {
		return "", "", fmt.Errorf("build input fingerprint unavailable: %w", err)
	}
	if unsupported {
		return "", "symlink or special file inputs", nil
	}
	return hex.EncodeToString(hash.Sum(nil)), "", nil
}

func offlineDockerfile(data string) bool {
	stages := map[string]bool{}
	hasFrom := false
	// Restrict the supported syntax instead of approximating Docker's parser.
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			directive := strings.TrimSpace(strings.TrimPrefix(line, "#"))
			key, _, hasEquals := strings.Cut(directive, "=")
			key = strings.ToLower(strings.TrimSpace(key))
			if hasEquals && (key == "syntax" || key == "escape") {
				return false
			}
			continue
		}
		if strings.Contains(line, "\\") {
			return false
		}
		fields := strings.Fields(line)
		instruction := strings.ToUpper(fields[0])
		switch instruction {
		case "FROM":
			if len(fields) != 2 && !(len(fields) == 4 && strings.EqualFold(fields[2], "AS")) {
				return false
			}
			ref := fields[1]
			if ref != "scratch" && !stages[ref] && !pinnedReference(ref) {
				return false
			}
			hasFrom = true
			if len(fields) == 4 {
				stages[fields[3]] = true
			}
		case "COPY":
			for _, field := range fields[1:] {
				if field == "--from" {
					return false
				}
				if strings.HasPrefix(field, "--from=") && !stages[strings.TrimPrefix(field, "--from=")] {
					return false
				}
				if strings.Contains(field, "$") {
					return false
				}
			}
		case "RUN":
			if strings.Contains(line, "--mount") || strings.Contains(line, "--network") {
				return false
			}
		case "ARG", "ENV", "WORKDIR", "USER", "LABEL", "EXPOSE", "ENTRYPOINT", "CMD", "SHELL", "STOPSIGNAL", "HEALTHCHECK":
		default:
			return false
		}
	}
	return hasFrom
}
func pinnedReference(ref string) bool {
	parts := strings.Split(ref, "@")
	return len(parts) == 2 && parts[0] != "" && !strings.Contains(parts[0], "$") && digestPattern.MatchString(parts[1])
}

// An inspection failure is not absence. Only a successful exact-tag inventory
// showing no image allows a build; uncertain or malformed evidence stops it.
type localBuildImage struct {
	ID           string `json:"Id"`
	OS           string `json:"Os"`
	Architecture string
	Variant      string
	Config       struct{ Labels map[string]string }
}

func (local localBuildImage) matches(platform, fingerprint string) bool {
	return fingerprint != "" && digestPattern.MatchString(local.ID) && platformName(local.OS, local.Architecture, local.Variant) == platform && local.Config.Labels[buildFingerprintLabel] == fingerprint
}
func (w Workflow) localBuildMatches(ctx context.Context, s Spec, image, fingerprint, store string) (bool, bool, error) {
	local, exists, err := w.inspectLocalBuild(ctx, s, image)
	if err != nil || !exists {
		return false, exists, err
	}
	return local.matches(s.Platform, fingerprint) && w.buildReceiptMatches(buildReceipt{Version: 1, Store: store, Image: image, Platform: s.Platform, Fingerprint: fingerprint, ImageID: local.ID}), true, nil
}
func (w Workflow) inspectLocalBuild(ctx context.Context, s Spec, image string) (localBuildImage, bool, error) {
	raw, err := w.Runner.Output(ctx, s.SourceDir, "docker", "image", "inspect", image)
	if err != nil {
		listed, listErr := w.Runner.Output(ctx, s.SourceDir, "docker", "image", "ls", "--no-trunc", "--quiet", "--filter", "reference="+image)
		if listErr != nil || strings.TrimSpace(string(listed)) != "" {
			return localBuildImage{}, false, fmt.Errorf("immutable image tag lookup uncertain for %s", image)
		}
		return localBuildImage{}, false, nil
	}
	var images []localBuildImage
	if json.Unmarshal(raw, &images) != nil || len(images) != 1 {
		return localBuildImage{}, true, fmt.Errorf("immutable image inspection invalid for %s", image)
	}
	return images[0], true, nil
}

// Pinned base descriptors still need local config evidence: inherited ONBUILD
// instructions can introduce remote inputs. Unknown bases disable reuse. The
// daemon version is part of build configuration, including its default frontend.
func (w Workflow) buildDependencyEvidence(ctx context.Context, s Spec) (string, string, string, string) {
	selected, err := w.Runner.Output(ctx, s.SourceDir, "docker", "context", "show")
	builder := strings.TrimSpace(string(selected))
	if err != nil || !contextPattern.MatchString(builder) {
		return "", "", "", "Docker context unavailable"
	}
	identity, err := w.Runner.Output(ctx, s.SourceDir, "docker", "info", "--format", "{{.ID}}")
	daemon := strings.TrimSpace(string(identity))
	if err != nil || !contextPattern.MatchString(daemon) {
		return "", "", "", "Docker store identity unavailable"
	}
	store := builder + "\x00" + daemon

	version, err := w.Runner.Output(ctx, s.SourceDir, "docker", "version", "--format", "{{json .Server}}")
	var server map[string]any
	if err != nil || json.Unmarshal(version, &server) != nil || len(server) == 0 {
		return "", "", "", "builder configuration unavailable"
	}
	data, err := os.ReadFile(s.Dockerfile)
	if err != nil {
		return "", "", "", "Dockerfile unavailable"
	}
	evidence := store + "\x00" + string(version)
	stages := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		ref := fields[1]
		if ref != "scratch" && !stages[ref] {
			raw, err := w.Runner.Output(ctx, s.SourceDir, "docker", "image", "inspect", ref)
			var bases []struct {
				OS           string `json:"Os"`
				Architecture string
				Variant      string
				RepoDigests  []string
				Config       struct{ OnBuild []string }
			}
			if err != nil || json.Unmarshal(raw, &bases) != nil || len(bases) != 1 {
				return "", "", "", "base config unavailable"
			}
			base := bases[0]
			matched := false
			for _, digest := range base.RepoDigests {
				if digest == ref {
					matched = true
				}
			}
			if !matched || len(base.Config.OnBuild) != 0 || platformName(base.OS, base.Architecture, base.Variant) != s.Platform {
				return "", "", "", "base digest/platform or inherited inputs unverified"
			}
			evidence += "\x00" + string(raw)
		}
		if len(fields) == 4 {
			stages[fields[3]] = true
		}
	}
	return evidence, builder, store, ""
}
func platformName(os, arch, variant string) string {
	result := os + "/" + arch
	if variant != "" {
		result += "/" + variant
	}
	return result
}

func (w Workflow) kindImagePresent(ctx context.Context, s Spec, node, image string) bool {
	if s.Platform == "" || !pinnedReference(image) {
		return false
	}
	raw, err := w.Runner.Output(ctx, s.SourceDir, "docker", "exec", node, "crictl", "inspecti", image)
	if err != nil {
		return false
	}
	var local struct {
		Status struct{ RepoDigests []string }
		Info   struct {
			ImageSpec struct {
				OS           string `json:"os"`
				Architecture string
				Variant      string
			}
		}
	}
	if json.Unmarshal(raw, &local) != nil || platformName(local.Info.ImageSpec.OS, local.Info.ImageSpec.Architecture, local.Info.ImageSpec.Variant) != s.Platform {
		return false
	}
	// CRI may canonicalize repository:tag@digest to repository@digest.
	canonical := image
	at := strings.LastIndex(image, "@")
	slash := strings.LastIndex(image[:at], "/")
	if colon := strings.LastIndex(image[:at], ":"); colon > slash {
		canonical = image[:colon] + image[at:]
	}
	matched := false
	for _, ref := range local.Status.RepoDigests {
		if ref == image || ref == canonical {
			matched = true
		}
	}
	if !matched {
		return false
	}
	return w.kindImageContentPresent(ctx, s, node, image)
}

func (w Workflow) kindImageContentPresent(ctx context.Context, s Spec, node, image string) bool {
	raw, err := w.Runner.Output(ctx, s.SourceDir, "docker", "exec", node, "ctr", "-n", "k8s.io", "images", "check", "--quiet", "name=="+image)
	if err != nil {
		return false
	}
	for _, ref := range strings.Fields(string(raw)) {
		if ref == image {
			return true
		}
	}
	return false
}

func (w Workflow) ensureKindImage(ctx context.Context, s Spec, node, image string) error {
	unlock, err := lockImage(ctx, "kind/"+node+"/"+image)
	if err != nil {
		return err
	}
	defer unlock()
	if !s.RefreshImages && w.kindImagePresent(ctx, s, node, image) {
		w.progress("reuse matching KIND digest on " + node)
		return nil
	}
	// This runtime needs containerd to fetch content before CRI registers the
	// reference for kubelet's imagePullPolicy=Never path.
	args := []string{"exec", node, "ctr", "-n", "k8s.io", "images", "pull", "--plain-http"}
	if s.Platform != "" {
		args = append(args, "--platform", s.Platform)
	}
	if err := w.run(ctx, s.SourceDir, "fetch digest through KIND containerd", "docker", append(args, image)...); err != nil {
		return err
	}
	// Retain the normal transfer pull (including its verifier policy). Its
	// unpacker may skip compressed blobs for existing snapshots. Only when the
	// exact reference is still incomplete, fetch the same pinned content before
	// CRI registration; never weaken the complete-content reuse/post-pull gate.
	if s.Platform != "" && pinnedReference(image) && !w.kindImageContentPresent(ctx, s, node, image) {
		if err := w.run(ctx, s.SourceDir, "restore incomplete KIND content", "docker", "exec", node, "ctr", "-n", "k8s.io", "content", "fetch", "--plain-http", "--platform", s.Platform, image); err != nil {
			return err
		}
	}
	if err := w.run(ctx, s.SourceDir, "register digest with KIND CRI", "docker", "exec", node, "crictl", "pull", image); err != nil {
		return err
	}
	if s.Platform != "" && pinnedReference(image) && !w.kindImagePresent(ctx, s, node, image) {
		return fmt.Errorf("KIND image digest/platform verification failed on %s", node)
	}
	return nil
}

// Hash only Docker/BuildKit build settings; raw environment values are never
// persisted in labels or logged. Sorting keeps the fingerprint stable.
func buildEnvironment() []string {
	var settings []string
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "DOCKER_") || strings.HasPrefix(item, "BUILDX_") || strings.HasPrefix(item, "BUILDKIT_") {
			settings = append(settings, item)
		}
	}
	sort.Strings(settings)
	return settings
}

// Docker otherwise auto-fills these arguments from credential-bearing client
// config. Explicit empty defaults keep offline inputs known without reading it.
func offlineBuildArgs(explicit map[string]string) map[string]string {
	effective := make(map[string]string, len(explicit)+10)
	for key, value := range explicit {
		effective[key] = value
	}
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "FTP_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "ftp_proxy", "no_proxy", "all_proxy"} {
		if _, ok := effective[key]; !ok {
			effective[key] = ""
		}
	}
	return effective
}
