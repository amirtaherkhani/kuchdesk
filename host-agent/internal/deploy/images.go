package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Serialize the immutable-tag check and build in this process. Independent
// operator processes still require serialization.
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
		defer imageLocks.Unlock()
		entry.users--
		if entry.users == 0 {
			delete(imageLocks.entries, key)
		}
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
	unlock, err := lockImage(ctx, s.KubeContext+"/k8s.io/"+image)
	if err != nil {
		return err
	}
	defer unlock()
	// Inventory must succeed: inspect errors alone cannot establish absence.
	raw, err := w.Runner.Output(ctx, s.SourceDir, "nerdctl", "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return errors.New("containerd immutable image tag inventory unavailable")
	}
	for _, candidate := range strings.Fields(string(raw)) {
		if candidate == "<none>:<none>" {
			continue
		}
		if strings.LastIndex(candidate, ":") <= strings.LastIndex(candidate, "/") {
			return errors.New("containerd immutable image inventory response invalid")
		}
		if canonicalImage(candidate) == canonicalImage(image) {
			return fmt.Errorf("immutable image tag %s already exists; refusing overwrite or unverified reuse", image)
		}
	}
	args := []string{"build", "--pull", "--file", s.Dockerfile, "--tag", image, "--platform", s.Platform}
	if s.Rebuild || s.RefreshImages {
		args = append(args, "--no-cache")
	}
	for _, key := range sortedBuildArgs(s.BuildArgs) {
		args = append(args, "--build-arg", key+"="+s.BuildArgs[key])
	}
	return w.run(ctx, s.SourceDir, "nerdctl build", "nerdctl", append(args, s.SourceDir)...)
}
func canonicalImage(ref string) string {
	if !strings.Contains(ref, "/") {
		return "docker.io/library/" + ref
	}
	first, _, _ := strings.Cut(ref, "/")
	if first == "index.docker.io" || first == "registry-1.docker.io" {
		return "docker.io/" + strings.TrimPrefix(ref, first+"/")
	}
	if !strings.ContainsAny(first, ".:") && first != "localhost" {
		return "docker.io/" + ref
	}
	return ref
}

func sortedBuildArgs(args map[string]string) []string {
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func (w Workflow) imageDigest(ctx context.Context, s Spec, image string) (string, error) {
	raw, err := w.Runner.Output(ctx, s.SourceDir, "nerdctl", "image", "inspect", "--mode=native", "--platform", s.Platform, image)
	if err != nil {
		return "", errors.New("containerd image digest lookup failed")
	}
	var images []struct {
		Image struct {
			Target struct {
				Digest string `json:"digest"`
			} `json:"target"`
		} `json:"Image"`
		ImageConfig struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
			Variant      string `json:"variant"`
		} `json:"ImageConfig"`
	}
	if json.Unmarshal(raw, &images) != nil || len(images) == 0 || !digestPattern.MatchString(images[0].Image.Target.Digest) {
		return "", errors.New("containerd manifest digest unavailable")
	}
	// nerdctl may return the tag and its config-ID alias for one lookup.
	// Every resolved record must agree on the manifest and selected platform.
	for _, candidate := range images {
		if candidate.Image.Target.Digest != images[0].Image.Target.Digest {
			return "", errors.New("containerd image records disagree on manifest digest")
		}
		config := candidate.ImageConfig
		platform := config.OS + "/" + config.Architecture
		if config.Variant != "" {
			platform += "/" + config.Variant
		}
		if platform != s.Platform {
			return "", errors.New("containerd image platform does not match deployment platform")
		}
	}
	// ctr prints only complete, unpacked refs in quiet mode. No archive/export
	// is created. The selected profile's native platform must be usable by K3s.
	complete, err := w.Runner.Output(ctx, s.SourceDir, "ctr", "images", "check", "--quiet", "name=="+canonicalImage(image))
	if err != nil {
		return "", errors.New("containerd content verification unavailable")
	}
	found := false
	for _, ref := range strings.Fields(string(complete)) {
		if canonicalImage(ref) == canonicalImage(image) {
			found = true
		}
	}
	if !found {
		return "", errors.New("containerd image content is incomplete or not unpacked")
	}
	return images[0].Image.Target.Digest, nil
}
