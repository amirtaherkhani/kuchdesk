package deploy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeFixture(digest, arch string) string {
	return `[{"Image":{"target":{"digest":"` + digest + `"}},"ImageConfigDesc":{"digest":"sha256:` + strings.Repeat("b", 64) + `"},"ImageConfig":{"os":"linux","architecture":"` + arch + `"}}]`
}

type inventoryRunner struct {
	fakeRunner
	inventory    []byte
	inventoryErr error
}

func (r *inventoryRunner) Output(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	if name == "nerdctl" && args[0] == "images" {
		return r.inventory, r.inventoryErr
	}
	return r.fakeRunner.Output(ctx, dir, name, args...)
}
func TestImmutableTagGateRequiresSuccessfulAbsentInventory(t *testing.T) {
	for _, tc := range []struct {
		name, inventory string
		err             error
		blocked         bool
	}{{"absent", "", nil, false}, {"present", "localhost:5001/kuchdesk-agent:git-123456789abc", nil, true}, {"uncertain", "", errors.New("store unavailable"), true}, {"malformed", "invalid-row", nil, true}} {
		t.Run(tc.name, func(t *testing.T) {
			s := fixture(t)
			r := &inventoryRunner{inventory: []byte(tc.inventory), inventoryErr: tc.err}
			err := (Workflow{Runner: r}).buildImage(context.Background(), s, s.ImageRepository+":"+s.ImageTag)
			if (err != nil) != tc.blocked {
				t.Fatalf("gate: %v", err)
			}
			if tc.blocked && len(r.calls) != 0 {
				t.Fatal("built after blocked inventory")
			}
		})
	}
}
func TestManifestDigestUsesContainerdTargetAndVerifiesPlatformContent(t *testing.T) {
	s := fixture(t)
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, raw, fail string
		bad             bool
	}{{"manifest", nativeFixture(digest, "arm64"), "", false}, {"matching-aliases", nativeRecords(nativeFixture(digest, "arm64"), nativeFixture(digest, "arm64")), "", false}, {"conflicting-aliases", nativeRecords(nativeFixture(digest, "arm64"), nativeFixture("sha256:"+strings.Repeat("c", 64), "arm64")), "", true}, {"conflicting-alias-platform", nativeRecords(nativeFixture(digest, "arm64"), nativeFixture(digest, "amd64")), "", true}, {"wrong-platform", nativeFixture(digest, "amd64"), "", true}, {"config-only", `[{"ImageConfigDesc":{"digest":"` + digest + `"}}]`, "", true}, {"missing-content", nativeFixture(digest, "arm64"), "ctr images", true}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRunner{digests: tc.raw, failAt: tc.fail}
			got, err := (Workflow{Runner: r}).imageDigest(context.Background(), s, s.ImageRepository+":"+s.ImageTag)
			if (err != nil) != tc.bad {
				t.Fatalf("verification: %v", err)
			}
			if !tc.bad && got != digest {
				t.Fatal("used config ID as manifest")
			}
		})
	}
}

type sourceRunner struct{ dirty, ignored string }

func (r sourceRunner) Run(context.Context, string, string, ...string) error { return nil }
func (r sourceRunner) Output(_ context.Context, _ string, name string, args ...string) ([]byte, error) {
	if name != "git" {
		return nil, errors.New("unexpected command")
	}
	switch args[0] {
	case "rev-parse":
		return []byte(strings.Repeat("a", 40)), nil
	case "status":
		return []byte(r.dirty), nil
	case "ls-files":
		return []byte(r.ignored), nil
	}
	return nil, errors.New("unexpected git command")
}
func TestDirtyIdentityHashesActualContextAndExcludesPrivateFiles(t *testing.T) {
	s := fixture(t)
	ignore := "**\n!Dockerfile\n!main.go\n!go.sum\n"
	if err := os.WriteFile(filepath.Join(s.SourceDir, ".dockerignore"), []byte(ignore), 0600); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(s.SourceDir, "values.local.yaml")
	os.WriteFile(private, []byte("fixture-private-one"), 0600)
	source := filepath.Join(s.SourceDir, "main.go")
	os.WriteFile(source, []byte("first"), 0600)
	r := sourceRunner{dirty: " M main.go", ignored: "values.local.yaml\x00"}
	first, err := SourceIdentity(context.Background(), r, s)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(private, []byte("fixture-private-two"), 0600)
	second, err := SourceIdentity(context.Background(), r, s)
	if err != nil || first != second {
		t.Fatal("private file affected identity")
	}
	os.WriteFile(source, []byte("second"), 0600)
	third, err := SourceIdentity(context.Background(), r, s)
	if err != nil || first.ImageTag == third.ImageTag || !strings.HasPrefix(third.ImageTag, "git-"+strings.Repeat("a", 12)+"-dirty-") {
		t.Fatal("dirty context did not change identity")
	}
	os.WriteFile(filepath.Join(s.SourceDir, ".dockerignore"), []byte(ignore+"!values.local.yaml\n"), 0600)
	if _, err := SourceIdentity(context.Background(), r, s); err == nil {
		t.Fatal("ignored private file accepted in build context")
	}
}
func TestSourceIdentityIncludesBuildSettingsAndRejectsSymlinks(t *testing.T) {
	s := fixture(t)
	first, err := SourceIdentity(context.Background(), sourceRunner{}, s)
	if err != nil {
		t.Fatal(err)
	}
	s.BuildArgs = map[string]string{"FEATURE": "yes"}
	second, err := SourceIdentity(context.Background(), sourceRunner{}, s)
	if err != nil || first.InputHash == second.InputHash {
		t.Fatal("build arguments not hashed")
	}
	if err := os.Symlink(s.Dockerfile, filepath.Join(s.SourceDir, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := SourceIdentity(context.Background(), sourceRunner{}, s); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestIgnoreRulesMatchRepositoryAllowlist(t *testing.T) {
	rules, err := ignoreRules("**\n!kuchdesk-agent/\n!kuchdesk-agent/**\n!configyaml/**\n**/values.local.yaml\n**/.git\n")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{"other/file": true, "kuchdesk-agent/main.go": false, "kuchdesk-agent/chart/values.local.yaml": true, "configyaml/load.go": false, ".git": true} {
		if ignoredBy(rules, path) != want {
			t.Fatalf("wrong exclusion for %s", path)
		}
	}
	if _, err := ignoreRules("[abc]"); err == nil {
		t.Fatal("unsupported ignore syntax accepted")
	}
}
func TestReuseBuildRejectedWithoutContainerdProvenance(t *testing.T) {
	s := fixture(t)
	s.ReuseBuild = true
	if err := s.Validate(); err == nil || !strings.Contains(err.Error(), "provenance") {
		t.Fatal("unverified build reuse accepted")
	}
}

func TestImageCanonicalizationPreservesTagIdentity(t *testing.T) {
	for _, ref := range []string{"fixture:v1", "library/fixture:v1", "docker.io/library/fixture:v1", "index.docker.io/library/fixture:v1"} {
		if got := canonicalImage(ref); got != "docker.io/library/fixture:v1" {
			t.Fatalf("unexpected canonical image: %s", got)
		}
	}
}

func nativeRecords(records ...string) string {
	var objects []string
	for _, record := range records {
		objects = append(objects, strings.TrimSuffix(strings.TrimPrefix(record, "["), "]"))
	}
	return "[" + strings.Join(objects, ",") + "]"
}
