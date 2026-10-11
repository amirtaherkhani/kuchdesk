package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Persistent fake stores at the real Runner seam; no image executable runs.
type imageStoreRunner struct {
	mu               sync.Mutex
	docker, cri      map[string][]byte
	ready            map[string]bool
	builds, pulls    map[string]int
	buildArgs        []string
	engine           string
	daemon           string
	configProxies    map[string]string
	effectiveProxies map[string]string
}

func newImageStoreRunner() *imageStoreRunner {
	return &imageStoreRunner{docker: map[string][]byte{}, cri: map[string][]byte{}, ready: map[string]bool{}, builds: map[string]int{}, pulls: map[string]int{}}
}
func (r *imageStoreRunner) Run(_ context.Context, _ string, name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "docker" && args[0] == "build" {
		var tag, label, platform string
		effective := map[string]string{}
		for i := 1; i < len(args)-1; i++ {
			switch args[i] {
			case "--build-arg":
				pair := strings.SplitN(args[i+1], "=", 2)
				if len(pair) == 2 {
					effective[pair[0]] = pair[1]
				}
			case "--tag":
				tag = args[i+1]
			case "--label":
				label = args[i+1]
			case "--platform":
				platform = args[i+1]
			}
		}
		for key, value := range r.configProxies {
			if _, explicit := effective[key]; !explicit {
				effective[key] = value
			}
		}
		r.effectiveProxies = effective
		r.builds[tag]++
		r.buildArgs = append([]string(nil), args...)
		parts := strings.Split(platform, "/")
		if len(parts) < 2 {
			parts = []string{"linux", "arm64"}
		}
		labels := map[string]string{}
		if label != "" {
			pair := strings.SplitN(label, "=", 2)
			labels[pair[0]] = pair[1]
		}
		raw, _ := json.Marshal([]any{map[string]any{"Id": "sha256:" + strings.Repeat("c", 64), "Os": parts[0], "Architecture": parts[1], "Config": map[string]any{"Labels": labels}}})
		r.docker[tag] = raw
		return nil
	}
	if name == "docker" && args[0] == "exec" {
		node := args[1]
		ref := args[len(args)-1]
		if args[2] == "ctr" {
			r.pulls[node+"/ctr"]++
			r.ready[node+"/"+ref] = true
		} else {
			r.pulls[node+"/cri"]++
			r.cri[node+"/"+ref] = criImage(ref, "linux/arm64")
		}
		return nil
	}
	return errors.New("unexpected command")
}
func (r *imageStoreRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "docker" && args[0] == "info" {
		if r.daemon != "" {
			return []byte(r.daemon), nil
		}
		return []byte("fixture-daemon"), nil
	}
	if name == "docker" && args[0] == "context" {
		return []byte("fixture-context"), nil
	}
	if name == "docker" && args[0] == "version" {
		if r.engine == "" {
			return []byte(`{"Version":"test-engine"}`), nil
		}
		return []byte(r.engine), nil
	}
	if name == "docker" && args[0] == "image" && args[1] == "ls" {
		return nil, nil
	}
	if name == "docker" && args[0] == "image" {
		if raw, ok := r.docker[args[len(args)-1]]; ok {
			return raw, nil
		}
		return nil, errors.New("absent")
	}
	if name == "kubectl" {
		return []byte(`{"items":[{"metadata":{"name":"node-a"}},{"metadata":{"name":"node-b"}}]}`), nil
	}
	if name == "docker" && args[0] == "exec" {
		node := args[1]
		if args[2] == "crictl" {
			if raw, ok := r.cri[node+"/"+args[len(args)-1]]; ok {
				return raw, nil
			}
			return nil, errors.New("absent")
		}
		var refs []string
		for key, complete := range r.ready {
			if complete && strings.HasPrefix(key, node+"/") {
				refs = append(refs, strings.TrimPrefix(key, node+"/"))
			}
		}
		return []byte(strings.Join(refs, "\n")), nil
	}
	return nil, errors.New("unexpected inspection")
}
func criImage(ref, platform string) []byte {
	parts := strings.Split(platform, "/")
	variant := ""
	if len(parts) > 2 {
		variant = parts[2]
	}
	raw, _ := json.Marshal(map[string]any{"status": map[string]any{"repoDigests": []string{ref}}, "info": map[string]any{"imageSpec": map[string]string{"os": parts[0], "architecture": parts[1], "variant": variant}}})
	return raw
}
func imageWorkflow(t *testing.T, r Runner) Workflow {
	t.Helper()
	return Workflow{Runner: r, BuildReceiptDir: filepath.Join(t.TempDir(), "receipts")}
}
func reusableSpec(t *testing.T) Spec {
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_BUILDKIT", "1")
	s := fixture(t)
	s.Platform = "linux/arm64"
	s.ReuseBuild = true
	if err := os.WriteFile(s.Dockerfile, []byte("FROM scratch\nCOPY . /src\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestBuildReusesOnlyMatchingInputs(t *testing.T) {
	for _, change := range []string{"unchanged", "source", "dependency", "dockerfile", "config", "argument", "platform", "missing", "missing-id", "rebuild", "refresh", "mutable-base", "implicit-platform", "engine"} {
		t.Run(change, func(t *testing.T) {
			s := reusableSpec(t)
			r := newImageStoreRunner()
			w := imageWorkflow(t, r)
			ctx := context.Background()
			image := s.ImageRepository + ":" + s.ImageTag
			if err := w.buildImage(ctx, s, image); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "source", "dependency", "config":
				file := map[string]string{"source": "main.go", "dependency": "go.sum", "config": ".dockerignore"}[change]
				if err := os.WriteFile(filepath.Join(s.SourceDir, file), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "dockerfile":
				if err := os.WriteFile(s.Dockerfile, []byte("FROM scratch\nCOPY . /different\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "argument":
				s.BuildArgs = map[string]string{"VERSION": "two"}
			case "platform":
				s.Platform = "linux/amd64"
			case "missing":
				delete(r.docker, image)
			case "missing-id":
				r.docker[image] = []byte(strings.ReplaceAll(string(r.docker[image]), "sha256:"+strings.Repeat("c", 64), ""))
			case "rebuild":
				s.Rebuild = true
				delete(r.docker, image)
			case "refresh":
				s.RefreshImages = true
				delete(r.docker, image)
			case "mutable-base":
				if err := os.WriteFile(s.Dockerfile, []byte("FROM golang:stable\nRUN echo hi\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "engine":
				r.engine = `{"Version":"updated-engine"}`
			case "implicit-platform":
				s.Platform = ""
			}
			// Changed build inputs require a new absent source tag, never an overwrite.
			if change != "unchanged" && change != "missing" && change != "missing-id" && change != "rebuild" && change != "refresh" {
				image = s.ImageRepository + ":git-223456789abc"
			}
			if change == "missing-id" {
				delete(r.docker, image)
			}
			if err := w.buildImage(ctx, s, image); err != nil {
				t.Fatal(err)
			}
			want := 2
			if change == "unchanged" {
				want = 1
			}
			total := 0
			for _, count := range r.builds {
				total += count
			}
			if total != want {
				t.Fatalf("builds=%d want=%d", total, want)
			}
			if change == "mutable-base" && !strings.Contains(strings.Join(r.buildArgs, " "), "--pull") {
				t.Fatal("mutable base build lost update check")
			}
		})
	}
}
func TestConcurrentBuildRequestsReuseOneBuild(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.buildImage(context.Background(), s, image); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if r.builds[image] != 1 {
		t.Fatalf("concurrent builds=%d", r.builds[image])
	}
}
func TestKindReuseRequiresDigestPlatformAndCompleteStoreOnEveryNode(t *testing.T) {
	for _, change := range []string{"unchanged", "host-only", "missing-digest", "platform", "missing-content", "refresh", "implicit-platform", "mutable-reference"} {
		t.Run(change, func(t *testing.T) {
			s := reusableSpec(t)
			r := newImageStoreRunner()
			w := imageWorkflow(t, r)
			ref := s.ImageRepository + ":" + s.ImageTag + "@sha256:" + strings.Repeat("a", 64)
			for _, node := range []string{"node-a", "node-b"} {
				r.cri[node+"/"+ref] = criImage(ref, s.Platform)
				r.ready[node+"/"+ref] = true
			}
			switch change {
			case "host-only":
				r.cri = map[string][]byte{}
				r.ready = map[string]bool{}
				r.docker[ref] = []byte("host has it")
			case "missing-digest":
				r.cri["node-b/"+ref] = criImage(strings.ReplaceAll(ref, strings.Repeat("a", 64), strings.Repeat("b", 64)), s.Platform)
			case "platform":
				r.cri["node-b/"+ref] = criImage(ref, "linux/amd64")
			case "missing-content":
				r.ready["node-b/"+ref] = false
			case "refresh":
				s.RefreshImages = true
			case "implicit-platform":
				s.Platform = ""
			case "mutable-reference":
				ref = s.ImageRepository + ":latest"
			}
			if err := w.prepullKind(context.Background(), s, ref); err != nil {
				t.Fatal(err)
			}
			wantA, wantB := 0, 0
			switch change {
			case "host-only", "refresh", "implicit-platform", "mutable-reference":
				wantA, wantB = 1, 1
			case "missing-digest", "platform", "missing-content":
				wantB = 1
			}
			if r.pulls["node-a/ctr"] != wantA || r.pulls["node-b/ctr"] != wantB || r.pulls["node-a/cri"] != wantA || r.pulls["node-b/cri"] != wantB {
				t.Fatalf("pulls=%v want a=%d b=%d", r.pulls, wantA, wantB)
			}
		})
	}
}
func TestConcurrentKindRequestsReuseEachNodeOnce(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	ref := s.ImageRepository + ":git-123456789abc@sha256:" + strings.Repeat("a", 64)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.prepullKind(context.Background(), s, ref); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for _, node := range []string{"node-a", "node-b"} {
		if r.pulls[node+"/ctr"] != 1 || r.pulls[node+"/cri"] != 1 {
			t.Fatalf("pulls=%v", r.pulls)
		}
	}
}

func TestOfflineBuildEligibilityRejectsUnknownDependencies(t *testing.T) {
	pin := "sha256:" + strings.Repeat("d", 64)
	for _, dockerfile := range []string{
		"# syntax=docker/dockerfile@" + pin + "\nFROM scratch", "FROM busybox:latest", "# syntax=docker/dockerfile:1\nFROM scratch", "FROM scratch\nADD https://example.test/file /file", "FROM scratch\nCOPY --from=busybox:latest /bin /bin", "FROM scratch\nRUN --mount=type=cache,target=/tmp echo hi", "FROM scratch\nRUN --network=host echo hi", "FROM $BASE", "FROM scratch\nCOPY $FILE /file", "FROM scratch\nONBUILD RUN echo hi", "FROM scratch\nRUN \\\n echo hi",
	} {
		t.Run(dockerfile, func(t *testing.T) {
			if offlineDockerfile(dockerfile) {
				t.Fatalf("accepted unresolved inputs: %s", dockerfile)
			}
		})
	}
	for _, dockerfile := range []string{"FROM scratch\nCOPY . /src", "FROM busybox@" + pin + " AS build\nRUN echo hi\nFROM scratch\nCOPY --from=build /bin /bin"} {
		if !offlineDockerfile(dockerfile) {
			t.Fatalf("rejected pinned inputs: %s", dockerfile)
		}
	}
}
func TestBuildFingerprintRejectsSymlinksAndIncludesModes(t *testing.T) {
	s := reusableSpec(t)
	file := filepath.Join(s.SourceDir, "input")
	if err := os.WriteFile(file, []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	first, _, err := buildFingerprint(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0700); err != nil {
		t.Fatal(err)
	}
	second, _, err := buildFingerprint(s)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("file mode was omitted")
	}
	if err := os.Symlink(file, filepath.Join(s.SourceDir, "link")); err != nil {
		t.Fatal(err)
	}
	fingerprint, reason, err := buildFingerprint(s)
	if err != nil || fingerprint != "" || reason == "" {
		t.Fatalf("symlink reused: %s %s %v", fingerprint, reason, err)
	}
}
func TestImageProfileRejectsInvalidPlatformAndBuildArgument(t *testing.T) {
	s := fixture(t)
	s.Platform = "linux/arm64,linux/amd64"
	if err := s.Validate(); err == nil {
		t.Fatal("accepted ambiguous platform")
	}
	s = fixture(t)
	s.BuildArgs = map[string]string{"--flag": "x"}
	if err := s.Validate(); err == nil {
		t.Fatal("accepted invalid build argument")
	}
}
func TestCanceledImageWaitDoesNotRunCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	unlock, err := lockImage(ctx, "held")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	cancel()
	if release, err := lockImage(ctx, "held"); err == nil {
		release()
		t.Fatal("canceled request acquired lock")
	}
}

func TestReuseBuildNeverOverwritesKnownImmutableTag(t *testing.T) {
	for _, change := range []string{"source", "platform", "cached-platform", "engine", "missing-id", "rebuild", "refresh", "mutable-base"} {
		t.Run(change, func(t *testing.T) {
			s := reusableSpec(t)
			r := newImageStoreRunner()
			w := imageWorkflow(t, r)
			image := s.ImageRepository + ":" + s.ImageTag
			if err := w.buildImage(context.Background(), s, image); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "source":
				if err := os.WriteFile(filepath.Join(s.SourceDir, "changed"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
			case "platform":
				s.Platform = "linux/amd64"
			case "cached-platform":
				r.docker[image] = []byte(strings.ReplaceAll(string(r.docker[image]), "arm64", "amd64"))
			case "engine":
				r.engine = `{"Version":"different"}`
			case "missing-id":
				r.docker[image] = []byte(`[{"Id":""}]`)
			case "rebuild":
				s.Rebuild = true
			case "refresh":
				s.RefreshImages = true
			case "mutable-base":
				if err := os.WriteFile(s.Dockerfile, []byte("FROM busybox:latest"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.buildImage(context.Background(), s, image); err == nil || !strings.Contains(err.Error(), "immutable") {
				t.Fatalf("accepted collision: %v", err)
			}
			if r.builds[image] != 1 {
				t.Fatalf("overwrote immutable tag: %d", r.builds[image])
			}
		})
	}
}
func TestReuseBuildRequiresVerifiedBaseConfig(t *testing.T) {
	for _, kind := range []string{"missing", "onbuild", "wrong-platform", "valid"} {
		t.Run(kind, func(t *testing.T) {
			s := reusableSpec(t)
			pin := "sha256:" + strings.Repeat("d", 64)
			base := "busybox@" + pin
			if err := os.WriteFile(s.Dockerfile, []byte("FROM "+base+"\nRUN echo hi\n"), 0600); err != nil {
				t.Fatal(err)
			}
			r := newImageStoreRunner()
			w := imageWorkflow(t, r)
			if kind != "missing" {
				arch := "arm64"
				onbuild := []string{}
				if kind == "wrong-platform" {
					arch = "amd64"
				}
				if kind == "onbuild" {
					onbuild = []string{"ADD https://example.test/remote /remote"}
				}
				raw, _ := json.Marshal([]any{map[string]any{"RepoDigests": []string{base}, "Os": "linux", "Architecture": arch, "Config": map[string]any{"OnBuild": onbuild}}})
				r.docker[base] = raw
			}
			image := s.ImageRepository + ":" + s.ImageTag
			if err := w.buildImage(context.Background(), s, image); err != nil {
				t.Fatal(err)
			}
			err := w.buildImage(context.Background(), s, image)
			if kind == "valid" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted unverified base reuse")
			}
		})
	}
}

type uncertainImageRunner struct {
	*imageStoreRunner
	inventory    []byte
	inventoryErr error
	malformed    bool
}

func (r uncertainImageRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if name == "docker" && args[0] == "image" {
		if args[1] == "ls" {
			return r.inventory, r.inventoryErr
		}
		if r.malformed {
			return []byte("not-json"), nil
		}
		return nil, errors.New("daemon lookup failed")
	}
	return r.imageStoreRunner.Output(ctx, dir, name, args...)
}
func TestImageLookupFailureCannotProveAbsence(t *testing.T) {
	for _, r := range []uncertainImageRunner{{inventory: []byte("sha256:present")}, {inventoryErr: errors.New("daemon unavailable")}, {malformed: true}} {
		r.imageStoreRunner = newImageStoreRunner()
		s := fixture(t)
		w := imageWorkflow(t, r)
		if err := w.buildImage(context.Background(), s, s.ImageRepository+":"+s.ImageTag); err == nil {
			t.Fatal("accepted unknown image absence")
		}
		if len(r.builds) != 0 {
			t.Fatal("built after unknown lookup")
		}
	}
}
func TestDefaultFreshBuildStopsAtExistingImmutableTag(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	s.ReuseBuild = false
	if err := w.buildImage(context.Background(), s, image); err == nil {
		t.Fatal("default path overwrote existing tag")
	}
	if r.builds[image] != 1 {
		t.Fatal("built despite collision")
	}
}
func TestBuildCommandRetainsExplicitInputsAndBypass(t *testing.T) {
	for _, bypass := range []string{"none", "rebuild", "refresh"} {
		t.Run(bypass, func(t *testing.T) {
			s := reusableSpec(t)
			s.BuildArgs = map[string]string{"VERSION": "1", "FEATURE": "yes"}
			s.Rebuild = bypass == "rebuild"
			s.RefreshImages = bypass == "refresh"
			r := newImageStoreRunner()
			w := imageWorkflow(t, r)
			if err := w.buildImage(context.Background(), s, s.ImageRepository+":"+s.ImageTag); err != nil {
				t.Fatal(err)
			}
			args := strings.Join(r.buildArgs, " ")
			for _, part := range []string{"--pull", "--builder fixture-context", "--network none", "--platform linux/arm64", "--build-arg FEATURE=yes", "--build-arg VERSION=1", "--label " + buildFingerprintLabel + "="} {
				if !strings.Contains(args, part) {
					t.Fatalf("lost %s in %s", part, args)
				}
			}
			if strings.Contains(args, "--no-cache") != (bypass != "none") {
				t.Fatalf("wrong bypass flags: %s", args)
			}
		})
	}
}
func TestFingerprintArgumentOrderIsStable(t *testing.T) {
	s := reusableSpec(t)
	s.BuildArgs = map[string]string{"B": "2", "A": "1"}
	first, _, err := buildFingerprint(s)
	if err != nil {
		t.Fatal(err)
	}
	s.BuildArgs = map[string]string{"A": "1", "B": "2"}
	second, _, err := buildFingerprint(s)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("argument map insertion order changed fingerprint")
	}
}
func TestKindReuseAcceptsCanonicalDigestOnlyWithExactPlatform(t *testing.T) {
	s := reusableSpec(t)
	s.ReuseBuild = false
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	digest := "sha256:" + strings.Repeat("a", 64)
	ref := s.ImageRepository + ":" + s.ImageTag + "@" + digest
	canonical := s.ImageRepository + "@" + digest
	r.cri["node-a/"+ref] = criImage(canonical, s.Platform)
	r.ready["node-a/"+ref] = true
	if !w.kindImagePresent(context.Background(), s, "node-a", ref) {
		t.Fatal("canonical digest was not reused independently of build opt-in")
	}
	r.cri["node-a/"+ref] = criImage(canonical, "linux/arm64/v8")
	if w.kindImagePresent(context.Background(), s, "node-a", ref) {
		t.Fatal("variant mismatch reused")
	}
}

func TestReuseRejectsReplacementIDWithPreservedFingerprint(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	r.docker[image] = []byte(strings.ReplaceAll(string(r.docker[image]), "sha256:"+strings.Repeat("c", 64), "sha256:"+strings.Repeat("e", 64)))
	if err := w.buildImage(context.Background(), s, image); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("replacement was reused: %v", err)
	}
	if r.builds[image] != 1 {
		t.Fatal("replacement was overwritten")
	}
}
func TestFrontendSelectorsDisableReuse(t *testing.T) {
	for _, directive := range []string{"# syntax = docker/dockerfile:1", "# SYNTAX\t=\tdocker/dockerfile:1", "#syntax=example.test/frontend:latest", "# escape = `", "# EsCaPe\t=\\"} {
		t.Run(directive, func(t *testing.T) {
			s := reusableSpec(t)
			if err := os.WriteFile(s.Dockerfile, []byte(directive+"\nFROM scratch\nCOPY . /src\n"), 0600); err != nil {
				t.Fatal(err)
			}
			fingerprint, reason, err := buildFingerprint(s)
			if err != nil || fingerprint != "" || reason == "" {
				t.Fatalf("frontend accepted: fingerprint=%s reason=%s err=%v", fingerprint, reason, err)
			}
		})
	}
	for _, value := range []string{"docker/dockerfile:1", "", "example.test/frontend@sha256:" + strings.Repeat("d", 64)} {
		t.Run("buildarg-"+value, func(t *testing.T) {
			s := reusableSpec(t)
			s.BuildArgs = map[string]string{"BUILDKIT_SYNTAX": value}
			fingerprint, reason, err := buildFingerprint(s)
			if err != nil || fingerprint != "" || reason == "" {
				t.Fatalf("custom frontend argument accepted: %s %s %v", fingerprint, reason, err)
			}
		})
	}
}
func TestOfflineBuildOverridesImplicitProxyConfig(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	proxies := []string{"HTTP_PROXY", "HTTPS_PROXY", "FTP_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "ftp_proxy", "no_proxy", "all_proxy"}
	r.configProxies = map[string]string{}
	for _, key := range proxies {
		r.configProxies[key] = "implicit-one"
	}
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	for _, key := range proxies {
		if value, explicit := r.effectiveProxies[key]; !explicit || value != "" {
			t.Fatalf("proxy %s not explicitly cleared: %q", key, value)
		}
	}
	for _, key := range proxies {
		r.configProxies[key] = "implicit-two"
	}
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	if r.builds[image] != 1 {
		t.Fatal("irrelevant implicit config prevented reuse")
	}
}
func TestExplicitProxyInputChangesFingerprintAndBuild(t *testing.T) {
	s := reusableSpec(t)
	s.BuildArgs = map[string]string{"HTTP_PROXY": "http://explicit-one", "no_proxy": "localhost"}
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	if r.effectiveProxies["HTTP_PROXY"] != "http://explicit-one" || r.effectiveProxies["no_proxy"] != "localhost" {
		t.Fatal("explicit proxies lost")
	}
	first, _, err := buildFingerprint(s)
	if err != nil {
		t.Fatal(err)
	}
	s.BuildArgs["HTTP_PROXY"] = "http://explicit-two"
	second, _, err := buildFingerprint(s)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("proxy input omitted")
	}
	if err := w.buildImage(context.Background(), s, image); err == nil {
		t.Fatal("proxy change reused existing immutable tag")
	}
	image = s.ImageRepository + ":git-223456789abc"
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	if r.effectiveProxies["HTTP_PROXY"] != "http://explicit-two" {
		t.Fatal("new explicit proxy not applied")
	}
}

func TestReuseRequiresPrivateValidReceipt(t *testing.T) {
	for _, change := range []string{"missing", "corrupt", "unknown-field", "trailing-json", "oversized", "missing-image-id", "wrong-id", "wrong-store", "wrong-image", "wrong-platform", "wrong-fingerprint", "world-readable", "world-readable-directory", "symlink"} {
		t.Run(change, func(t *testing.T) {
			s := reusableSpec(t)
			r := newImageStoreRunner()
			w := imageWorkflow(t, r)
			image := s.ImageRepository + ":" + s.ImageTag
			if err := w.buildImage(context.Background(), s, image); err != nil {
				t.Fatal(err)
			}
			files, err := os.ReadDir(w.BuildReceiptDir)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 {
				t.Fatalf("verified build should record one receipt, found %d", len(files))
			}
			path := filepath.Join(w.BuildReceiptDir, files[0].Name())
			switch change {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown-field":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var receipt map[string]any
				if err := json.Unmarshal(data, &receipt); err != nil {
					t.Fatal(err)
				}
				receipt["unknown"] = "unsupported"
				data, err = json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-image-id":
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var receipt map[string]any
				if err := json.Unmarshal(data, &receipt); err != nil {
					t.Fatal(err)
				}
				delete(receipt, "imageId")
				data, err = json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			case "trailing-json":
				file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := file.WriteString("{}")
				closeErr := file.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatal("failed to append test JSON")
				}
			case "oversized":
				if err := os.WriteFile(path, []byte(strings.Repeat(" ", 5000)), 0600); err != nil {
					t.Fatal(err)
				}
			case "world-readable-directory":
				if err := os.Chmod(w.BuildReceiptDir, 0755); err != nil {
					t.Fatal(err)
				}
			case "world-readable":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				other := filepath.Join(t.TempDir(), "receipt")
				if err := os.WriteFile(other, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(other, path); err != nil {
					t.Fatal(err)
				}
			default:
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var receipt map[string]any
				if err := json.Unmarshal(data, &receipt); err != nil {
					t.Fatal(err)
				}
				field := map[string]string{"wrong-id": "imageId", "wrong-store": "store", "wrong-image": "image", "wrong-platform": "platform", "wrong-fingerprint": "fingerprint"}[change]
				receipt[field] = "different"
				data, err = json.Marshal(receipt)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.buildImage(context.Background(), s, image); err == nil || !strings.Contains(err.Error(), "immutable") {
				t.Fatalf("accepted %s receipt: %v", change, err)
			}
			if r.builds[image] != 1 {
				t.Fatal("overwrote image after lost provenance")
			}
		})
	}
}
func TestReceiptPersistsAcrossWorkflowInstances(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	fresh := Workflow{Runner: r, BuildReceiptDir: w.BuildReceiptDir}
	if err := fresh.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	if r.builds[image] != 1 {
		t.Fatal("persistent receipt did not permit reuse")
	}
}

func TestBuildReceiptIsBoundToActualDaemon(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	r.daemon = "another-daemon"
	if err := w.buildImage(context.Background(), s, image); err == nil {
		t.Fatal("receipt reused in another Docker store")
	}
	if r.builds[image] != 1 {
		t.Fatal("tag overwritten in another store")
	}
}
func TestDefaultBuildRetainsClientProxyBehavior(t *testing.T) {
	s := reusableSpec(t)
	s.ReuseBuild = false
	r := newImageStoreRunner()
	r.configProxies = map[string]string{"HTTP_PROXY": "implicit-client-value"}
	w := imageWorkflow(t, r)
	if err := w.buildImage(context.Background(), s, s.ImageRepository+":"+s.ImageTag); err != nil {
		t.Fatal(err)
	}
	if r.effectiveProxies["HTTP_PROXY"] != "implicit-client-value" {
		t.Fatal("default build lost client proxy behavior")
	}
	if _, err := os.Stat(w.BuildReceiptDir); !os.IsNotExist(err) {
		t.Fatalf("default build wrote receipt metadata: %v", err)
	}
}
func TestReceiptPublicationUsesPrivateFilesAndAtomicReplacement(t *testing.T) {
	s := reusableSpec(t)
	r := newImageStoreRunner()
	w := imageWorkflow(t, r)
	image := s.ImageRepository + ":" + s.ImageTag
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(w.BuildReceiptDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("unexpected leftover receipt temporaries: %d", len(files))
	}
	info, err := os.Stat(filepath.Join(w.BuildReceiptDir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("receipt mode=%o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(w.BuildReceiptDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Fatalf("receipt dir mode=%o", dirInfo.Mode().Perm())
	}
	// A confirmed absent target can be built again; the same receipt key is
	// replaced atomically, without accumulating another provenance record.
	delete(r.docker, image)
	s.Rebuild = true
	if err := w.buildImage(context.Background(), s, image); err != nil {
		t.Fatal(err)
	}
	files, err = os.ReadDir(w.BuildReceiptDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("leftover records after replacement: %d", len(files))
	}
}

type invalidBuiltImageRunner struct {
	*imageStoreRunner
	failure string
}

func (r invalidBuiltImageRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	if err := r.imageStoreRunner.Run(ctx, dir, name, args...); err != nil {
		return err
	}
	if name == "docker" && args[0] == "build" {
		r.mu.Lock()
		defer r.mu.Unlock()
		for image, raw := range r.docker {
			switch r.failure {
			case "id":
				raw = []byte(strings.ReplaceAll(string(raw), "sha256:"+strings.Repeat("c", 64), "invalid"))
			case "platform":
				raw = []byte(strings.ReplaceAll(string(raw), "arm64", "amd64"))
			case "label":
				raw = []byte(strings.ReplaceAll(string(raw), buildFingerprintLabel, "wrong-label"))
			}
			r.docker[image] = raw
		}
	}
	return nil
}
func TestUnverifiedBuildCannotCreateReceipt(t *testing.T) {
	for _, failure := range []string{"id", "platform", "label"} {
		t.Run(failure, func(t *testing.T) {
			s := reusableSpec(t)
			r := invalidBuiltImageRunner{imageStoreRunner: newImageStoreRunner(), failure: failure}
			w := imageWorkflow(t, r)
			if err := w.buildImage(context.Background(), s, s.ImageRepository+":"+s.ImageTag); err == nil || !strings.Contains(err.Error(), "verification") {
				t.Fatalf("unverified build accepted: %v", err)
			}
			if _, err := os.Stat(w.BuildReceiptDir); !os.IsNotExist(err) {
				t.Fatalf("unverified build recorded provenance: %v", err)
			}
		})
	}
}
