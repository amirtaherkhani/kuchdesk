package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Explicit operator opt-in only. This exercises the real helper/ExecRunner,
// retains two tiny local scratch images and their source/receipts, and may
// repair missing content for the explicitly named existing agent digest.
// It never applies Helm, starts containers, removes images or changes access.
func TestLiveImageReuseAcceptance(t *testing.T) {
	if os.Getenv("KUCHDESK_LIVE_IMAGE_ACCEPTANCE") != "confirm-scratch-builds-and-current-agent-pull" {
		t.Skip("explicit live image acceptance opt-in required")
	}
	root := os.Getenv("KUCHDESK_LIVE_IMAGE_ARTIFACT_DIR")
	node := os.Getenv("KUCHDESK_LIVE_KIND_NODE")
	image := os.Getenv("KUCHDESK_LIVE_KIND_IMAGE")
	if !filepath.IsAbs(root) || !namePattern.MatchString(node) || !strings.HasPrefix(image, "localhost:5001/kuchdesk-agent:") || !pinnedReference(image) {
		t.Fatal("explicit artifact directory, existing node and pinned local agent image required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.MkdirTemp(root, "scratch-source-")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	runner := &liveImageRunner{fixtureRepository: "kuchdesk/reuse-acceptance", node: node, nodeImage: image, maxBuilds: 2, counts: map[string]int{}}
	w := Workflow{Runner: runner, BuildReceiptDir: filepath.Join(root, filepath.Base(fixture)+"-receipts")}
	s := Spec{SourceDir: fixture, Dockerfile: filepath.Join(fixture, "Dockerfile"), Platform: "linux/arm64", ReuseBuild: true, BuildArgs: map[string]string{"FEATURE": "initial"}}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(fixture, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("Dockerfile", "FROM scratch\nARG FEATURE\nLABEL org.opencontainers.image.version=\"reuse-acceptance\" io.kuchdesk.acceptance.feature=$FEATURE\nCOPY payload /payload\n")
	write("payload", "initial\n")
	write("build-config.json", "{\"FEATURE\":\"initial\"}\n")
	write("fixture-session", filepath.Base(fixture)+"\n")
	git := func(args ...string) string {
		t.Helper()
		raw, err := (ExecRunner{}).Output(ctx, fixture, "git", args...)
		if err != nil {
			t.Fatalf("fixture git command failed: %v", err)
		}
		return strings.TrimSpace(string(raw))
	}
	git("init", "--quiet")
	commit := func(message string) string {
		t.Helper()
		git("add", ".")
		git("-c", "user.name=KuchDesk Acceptance", "-c", "user.email=acceptance@localhost", "commit", "--quiet", "-m", message)
		sha := git("rev-parse", "HEAD")
		if git("status", "--porcelain", "--untracked-files=all") != "" {
			t.Fatal("fixture source must be clean")
		}
		return sha
	}
	firstSource := commit("test: initial offline scratch inputs")
	firstImage := runner.fixtureRepository + ":git-" + firstSource[:12]
	concurrent := func(operation func() error) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, 3)
		for i := 0; i < 3; i++ {
			go func() { <-start; results <- operation() }()
		}
		close(start)
		for i := 0; i < 3; i++ {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
	}
	concurrent(func() error { return w.buildImage(ctx, s, firstImage) })
	if runner.count("build") != 1 {
		t.Fatalf("concurrent initial build count = %d, want 1", runner.count("build"))
	}
	firstID := liveFixtureIdentity(t, ctx, fixture, firstImage, "initial", "initial\n")
	if err := w.buildImage(ctx, s, firstImage); err != nil {
		t.Fatal(err)
	}
	if runner.count("build") != 1 || liveFixtureIdentity(t, ctx, fixture, firstImage, "initial", "initial\n") != firstID {
		t.Fatal("unchanged inputs did not reuse the exact image")
	}
	write("payload", "changed\n")
	write("build-config.json", "{\"FEATURE\":\"changed\"}\n")
	s.BuildArgs = map[string]string{"FEATURE": "changed"}
	secondSource := commit("test: changed offline scratch inputs")
	secondImage := runner.fixtureRepository + ":git-" + secondSource[:12]
	if err := w.buildImage(ctx, s, firstImage); err == nil || runner.count("build") != 1 {
		t.Fatal("changed inputs must reject the existing immutable tag")
	}
	concurrent(func() error { return w.buildImage(ctx, s, secondImage) })
	secondID := liveFixtureIdentity(t, ctx, fixture, secondImage, "changed", "changed\n")
	if secondID == firstID || runner.count("build") != 2 {
		t.Fatal("changed committed inputs must build one new exact artifact")
	}
	if err := w.buildImage(ctx, s, secondImage); err != nil || runner.count("build") != 2 {
		t.Fatalf("changed fixture repeat failed to reuse: %v", err)
	}
	// The host fixtures are local-only. The existing deployed agent is the
	// separate required KIND store target; do not load fixtures into the node.
	nodeSpec := Spec{SourceDir: fixture, Platform: "linux/arm64"}
	nodeBefore := w.kindImagePresent(ctx, nodeSpec, node, image)
	concurrent(func() error { return w.ensureKindImage(ctx, nodeSpec, node, image) })
	if !w.kindImagePresent(ctx, nodeSpec, node, image) {
		t.Fatal("node digest/platform/content not verified")
	}
	wantPulls := 0
	if !nodeBefore {
		wantPulls = 1
	}
	if runner.count("ctr-pull") != wantPulls || runner.count("cri-pull") != wantPulls {
		t.Fatalf("concurrent node operations: %v, want %d each", runner.snapshot(), wantPulls)
	}
	if err := w.ensureKindImage(ctx, nodeSpec, node, image); err != nil || runner.count("ctr-pull") != wantPulls || runner.count("cri-pull") != wantPulls {
		t.Fatalf("complete node repeat did not reuse: %v", err)
	}
	evidence := map[string]any{"utc": time.Now().UTC(), "passed": true, "fixtureSource": fixture, "platform": s.Platform, "firstSourceCommit": firstSource, "firstImage": firstImage, "firstStoreImageID": firstID, "secondSourceCommit": secondSource, "secondImage": secondImage, "secondStoreImageID": secondID, "verifiedPayloads": []string{"initial\n", "changed\n"}, "kindNode": node, "kindImage": image, "kindInitiallyComplete": nodeBefore, "kindDigestPlatformContentVerified": true, "concurrentCallers": 3, "operations": runner.snapshot(), "removedImages": []string{}, "fixtureSourcesAndImagesRetained": true, "runtimeServiceRestarted": false, "helmApplied": false}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "live-acceptance.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("real helper acceptance passed; operations=%v; evidence retained in %s", runner.snapshot(), root)
}

type liveImageRunner struct {
	mu                sync.Mutex
	fixtureRepository string
	node, nodeImage   string
	counts            map[string]int
	maxBuilds         int
}

func (r *liveImageRunner) Run(ctx context.Context, dir, name string, args ...string) error {
	key := ""
	if name == "docker" && len(args) > 1 && args[0] == "build" {
		for i := range args {
			if args[i] == "--tag" && i+1 < len(args) && strings.HasPrefix(args[i+1], r.fixtureRepository+":git-") {
				key = "build"
			}
		}
	} else if name == "docker" && len(args) > 4 && args[0] == "exec" && args[1] == r.node && args[len(args)-1] == r.nodeImage {
		if args[2] == "ctr" && strings.Contains(strings.Join(args[3:], " "), "images pull --plain-http --platform linux/arm64 ") {
			key = "ctr-pull"
		} else if args[2] == "ctr" && strings.Contains(strings.Join(args[3:], " "), "content fetch --plain-http --platform linux/arm64 ") {
			key = "content-fetch"
		} else if args[2] == "crictl" && len(args) == 5 && args[3] == "pull" {
			key = "cri-pull"
		}
	}
	if key == "" {
		return fmt.Errorf("live acceptance refuses this mutation")
	}
	r.mu.Lock()
	r.counts[key]++
	n := r.counts[key]
	r.mu.Unlock()
	if key == "build" && n > r.maxBuilds {
		return fmt.Errorf("live acceptance refuses additional scratch builds")
	}
	if key != "build" && n > 1 {
		return fmt.Errorf("live acceptance refuses redundant node pull")
	}
	return (ExecRunner{}).Run(ctx, dir, name, args...)
}
func (r *liveImageRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	return (ExecRunner{}).Output(ctx, dir, name, args...)
}
func (r *liveImageRunner) count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[key]
}
func (r *liveImageRunner) snapshot() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := map[string]int{}
	for key, value := range r.counts {
		result[key] = value
	}
	return result
}

// Resume against retained inputs after a diagnosed node issue. Every real
// build is forbidden: accepted fixtures are not rebuilt unnecessarily.
func TestLiveReuseResumeAcceptance(t *testing.T) {
	if os.Getenv("KUCHDESK_LIVE_IMAGE_ACCEPTANCE") != "confirm-scratch-builds-and-current-agent-pull" {
		t.Skip("explicit live acceptance opt-in required")
	}
	root := os.Getenv("KUCHDESK_LIVE_IMAGE_ARTIFACT_DIR")
	fixture := os.Getenv("KUCHDESK_LIVE_FIXTURE_SOURCE")
	node := os.Getenv("KUCHDESK_LIVE_KIND_NODE")
	image := os.Getenv("KUCHDESK_LIVE_KIND_IMAGE")
	if !filepath.IsAbs(root) || filepath.Dir(fixture) != root || !strings.HasPrefix(filepath.Base(fixture), "scratch-source-") || !namePattern.MatchString(node) || !strings.HasPrefix(image, "localhost:5001/kuchdesk-agent:") || !pinnedReference(image) {
		t.Fatal("exact retained fixture and pinned existing agent required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	raw, err := (ExecRunner{}).Output(ctx, fixture, "git", "rev-list", "--reverse", "HEAD")
	commits := strings.Fields(string(raw))
	if err != nil || len(commits) != 2 || len(commits[0]) != 40 || len(commits[1]) != 40 {
		t.Fatal("retained source revisions unavailable")
	}
	r := &liveImageRunner{fixtureRepository: "kuchdesk/reuse-acceptance", node: node, nodeImage: image, counts: map[string]int{}}
	w := Workflow{Runner: r, BuildReceiptDir: filepath.Join(root, filepath.Base(fixture)+"-receipts")}
	s := Spec{SourceDir: fixture, Dockerfile: filepath.Join(fixture, "Dockerfile"), Platform: "linux/arm64", ReuseBuild: true, BuildArgs: map[string]string{"FEATURE": "changed"}}
	first := r.fixtureRepository + ":git-" + commits[0][:12]
	second := r.fixtureRepository + ":git-" + commits[1][:12]
	firstID := liveFixtureIdentity(t, ctx, fixture, first, "initial", "initial\n")
	secondID := liveFixtureIdentity(t, ctx, fixture, second, "changed", "changed\n")
	if firstID == secondID {
		t.Fatal("different inputs did not produce different content")
	}
	concurrent := func(op func() error) {
		t.Helper()
		start := make(chan struct{})
		results := make(chan error, 3)
		for i := 0; i < 3; i++ {
			go func() { <-start; results <- op() }()
		}
		close(start)
		for i := 0; i < 3; i++ {
			if err := <-results; err != nil {
				t.Fatal(err)
			}
		}
	}
	concurrent(func() error { return w.buildImage(ctx, s, second) })
	if r.count("build") != 0 {
		t.Fatal("retained unchanged inputs attempted a build")
	}
	if err := w.buildImage(ctx, s, first); err == nil || r.count("build") != 0 {
		t.Fatal("altered input reused/overwrote the original immutable tag")
	}
	nodeSpec := Spec{SourceDir: fixture, Platform: "linux/arm64"}
	before := w.kindImagePresent(ctx, nodeSpec, node, image)
	concurrent(func() error { return w.ensureKindImage(ctx, nodeSpec, node, image) })
	if !w.kindImagePresent(ctx, nodeSpec, node, image) {
		t.Fatal("node digest/platform/content not verified")
	}
	want := 0
	if !before {
		want = 1
	}
	if r.count("ctr-pull") != want || r.count("cri-pull") != want || r.count("content-fetch") > want {
		t.Fatalf("redundant concurrent operations: %v", r.snapshot())
	}
	if err := w.ensureKindImage(ctx, nodeSpec, node, image); err != nil || r.count("ctr-pull") != want || r.count("cri-pull") != want {
		t.Fatalf("complete node was not reused: %v", err)
	}
	data, _ := json.MarshalIndent(map[string]any{"utc": time.Now().UTC(), "passed": true, "fixtureSource": fixture, "sourceCommits": commits, "hostImages": []map[string]string{{"image": first, "storeImageID": firstID, "verifiedPayload": "initial\n"}, {"image": second, "storeImageID": secondID, "verifiedPayload": "changed\n"}}, "platform": s.Platform, "kindNode": node, "kindImage": image, "kindInitiallyComplete": before, "kindDigestPlatformContentVerified": true, "concurrentCallers": 3, "operations": r.snapshot(), "firstRunHostBuilds": 2, "firstRunFailure": "missing compressed blob after transfer pull; see live-test-initial.log", "removedImages": []string{}, "fixtureSourcesAndImagesRetained": true, "runtimeServiceRestarted": false, "helmApplied": false}, "", "  ")
	if err := os.WriteFile(filepath.Join(root, "live-acceptance.json"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("retained fixture/node live acceptance passed; operations=%v", r.snapshot())
}

func liveFixtureIdentity(t *testing.T, ctx context.Context, dir, image, feature, payload string) string {
	t.Helper()
	raw, err := (ExecRunner{}).Output(ctx, dir, "docker", "image", "inspect", image)
	var local []localBuildImage
	if err != nil || json.Unmarshal(raw, &local) != nil || len(local) != 1 || platformName(local[0].OS, local[0].Architecture, local[0].Variant) != "linux/arm64" || !digestPattern.MatchString(local[0].ID) || local[0].Config.Labels["io.kuchdesk.acceptance.feature"] != feature {
		t.Fatal("fixture image ID/platform/effective arguments unverified")
	}
	archive, err := (ExecRunner{}).Output(ctx, dir, "docker", "image", "save", "--platform", "linux/arm64", image)
	if err != nil || len(archive) > 16<<20 {
		t.Fatal("bounded fixture image content unavailable")
	}
	files := map[string][]byte{}
	outer := tar.NewReader(bytes.NewReader(archive))
	for {
		h, err := outer.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			files[h.Name], err = io.ReadAll(io.LimitReader(outer, 16<<20))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	var manifests []struct{ Layers []string }
	if json.Unmarshal(files["manifest.json"], &manifests) != nil || len(manifests) != 1 {
		t.Fatal("fixture archive manifest unavailable")
	}
	found := false
	for _, layer := range manifests[0].Layers {
		data, ok := files[layer]
		if !ok {
			t.Fatal("fixture layer absent")
		}
		var reader io.Reader = bytes.NewReader(data)
		if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
			gzipReader, err := gzip.NewReader(reader)
			if err != nil {
				t.Fatal(err)
			}
			defer gzipReader.Close()
			reader = gzipReader
		}
		inner := tar.NewReader(reader)
		for {
			h, err := inner.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimPrefix(h.Name, "./") == "payload" {
				actual, err := io.ReadAll(io.LimitReader(inner, 1024))
				if err != nil || string(actual) != payload {
					t.Fatal("built fixture payload mismatch")
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("built fixture payload absent")
	}
	return local[0].ID
}
