package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Identity struct {
	SourceCommit string `json:"sourceCommit"`
	InputHash    string `json:"inputHash"`
	ImageTag     string `json:"imageTag"`
}

// SourceIdentity supports the explicitly authorized dirty-source deployment:
// git-<HEAD>-dirty-<input hash>. Ignored private files must be outside the build
// context; no secret file content is read to establish identity.
func SourceIdentity(ctx context.Context, runner Runner, s Spec) (Identity, error) {
	resolvedSource, err := filepath.EvalSymlinks(s.SourceDir)
	if err != nil {
		return Identity{}, errors.New("source directory unavailable")
	}
	s.SourceDir = resolvedSource
	resolvedDockerfile, err := filepath.EvalSymlinks(s.Dockerfile)
	if err != nil {
		return Identity{}, errors.New("Dockerfile unavailable")
	}
	s.Dockerfile = resolvedDockerfile
	raw, err := runner.Output(ctx, s.SourceDir, "git", "rev-parse", "HEAD")
	head := strings.TrimSpace(string(raw))
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{40,64}$`).MatchString(head) {
		return Identity{}, errors.New("full source HEAD unavailable")
	}
	changed, err := runner.Output(ctx, s.SourceDir, "git", "status", "--porcelain", "--untracked-files=all", "--", ".")
	if err != nil {
		return Identity{}, errors.New("source state unavailable")
	}
	ignored, err := runner.Output(ctx, s.SourceDir, "git", "ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--", ".")
	if err != nil {
		return Identity{}, errors.New("ignored build input inventory unavailable")
	}
	private := map[string]bool{}
	for _, path := range strings.Split(string(ignored), "\x00") {
		if path != "" {
			private[filepath.ToSlash(path)] = true
		}
	}
	ignorePath := filepath.Join(s.SourceDir, ".dockerignore")
	if _, err := os.Stat(s.Dockerfile + ".dockerignore"); err == nil {
		ignorePath = s.Dockerfile + ".dockerignore"
	} else if !os.IsNotExist(err) {
		return Identity{}, errors.New("Dockerfile ignore rules unavailable")
	}
	ignore, err := os.ReadFile(ignorePath)
	if err != nil && !os.IsNotExist(err) {
		return Identity{}, errors.New("build ignore rules unavailable")
	}
	rules, err := ignoreRules(string(ignore))
	if err != nil {
		return Identity{}, err
	}
	hash := sha256.New()
	dockerfile, _ := filepath.Rel(s.SourceDir, s.Dockerfile)
	metadata, _ := json.Marshal(struct {
		Version, Head, Dockerfile, Platform, Context, Ignore string
		Args                                                 map[string]string
	}{"dirty-v1", head, filepath.ToSlash(dockerfile), s.Platform, "colima-containerd/k8s.io", string(ignore), s.BuildArgs})
	hash.Write(metadata)
	err = filepath.WalkDir(s.SourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(s.SourceDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		excluded := ignoredBy(rules, rel)
		// BuildKit sends the active Dockerfile and ignore file even if excluded.
		if excluded && path != s.Dockerfile && path != ignorePath {
			return nil
		}
		if rel == ".git" || strings.HasPrefix(rel, ".git/") || private[rel] {
			return errors.New("Git metadata or ignored files are included in the build context; exclude them in .dockerignore")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("symlink or special build input is unsupported")
		}
		fmt.Fprintf(hash, "\x00%s\x00%d\x00%d\x00", rel, info.Mode().Perm(), info.Size())
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
	})
	if err != nil {
		return Identity{}, fmt.Errorf("build input identity unavailable: %w", err)
	}
	identity := Identity{SourceCommit: head, InputHash: hex.EncodeToString(hash.Sum(nil)), ImageTag: "git-" + head[:12]}
	if len(strings.TrimSpace(string(changed))) != 0 {
		identity.ImageTag += "-dirty-" + identity.InputHash[:12]
	}
	return identity, nil
}

type ignoreRule struct {
	match   *regexp.Regexp
	include bool
}

// Support the repository's *, **, ?, path and negation rules. Reject more
// complex syntax instead of hashing a context that differs from BuildKit's.
func ignoreRules(data string) ([]ignoreRule, error) {
	var rules []ignoreRule
	for _, line := range strings.Split(strings.TrimPrefix(data, "\ufeff"), "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		include := strings.HasPrefix(line, "!")
		if include {
			line = strings.TrimPrefix(line, "!")
		}
		line = filepath.ToSlash(filepath.Clean(line))
		line = strings.Trim(line, "/")
		if line == "." {
			continue
		}
		if line == "" || strings.ContainsAny(line, "[]\\") || line == ".." || strings.HasPrefix(line, "../") {
			return nil, errors.New("unsupported .dockerignore pattern; use simple paths, *, **, ? and !")
		}
		var expr strings.Builder
		expr.WriteString("^")
		for i := 0; i < len(line); i++ {
			switch line[i] {
			case '*':
				if i+1 < len(line) && line[i+1] == '*' {
					i++
					if i+1 < len(line) && line[i+1] == '/' {
						i++
						expr.WriteString("(?:.*/)?")
					} else {
						expr.WriteString(".*")
					}
				} else {
					expr.WriteString("[^/]*")
				}
			case '?':
				expr.WriteString("[^/]")
			default:
				expr.WriteString(regexp.QuoteMeta(string(line[i])))
			}
		}
		expr.WriteString("(?:/.*)?$")
		compiled, err := regexp.Compile(expr.String())
		if err != nil {
			return nil, errors.New("invalid .dockerignore pattern")
		}
		rules = append(rules, ignoreRule{compiled, include})
	}
	return rules, nil
}
func ignoredBy(rules []ignoreRule, path string) bool {
	excluded := false
	for _, rule := range rules {
		if rule.match.MatchString(path) {
			excluded = !rule.include
		}
	}
	return excluded
}
