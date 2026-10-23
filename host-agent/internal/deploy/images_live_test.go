package deploy

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt in only against an explicitly selected existing local image. This probe
// performs Git/file reads, native image inspection and ctr content checks only.
func TestExistingImageReadOnly(t *testing.T) {
	profile := os.Getenv("KUCHDESK_DEPLOY_VERIFY_PROFILE")
	if profile == "" {
		t.Skip("set KUCHDESK_DEPLOY_VERIFY_PROFILE for existing-image read-only verification")
	}
	s, err := LoadSpec(profile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	runner := contextRunner{Runner: ExecRunner{}, context: s.KubeContext}
	identity, err := SourceIdentity(ctx, runner, s)
	if err != nil {
		t.Fatalf("source identity: %v", err)
	}
	t.Logf("sourceCommit=%s inputHash=%s currentTag=%s profileTag=%s", identity.SourceCommit, identity.InputHash, identity.ImageTag, s.ImageTag)
	digest, err := (Workflow{Runner: runner}).imageDigest(ctx, s, s.ImageRepository+":"+s.ImageTag)
	if err != nil {
		t.Fatalf("image verification: %v", err)
	}
	t.Logf("existing manifest=%s platform=%s content=complete/unpacked", digest, s.Platform)
}
