package infisical

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
)

var ErrVersionConflict = errors.New("secret version precondition failed")

type GroupChange struct {
	Name            string  `json:"name"`
	Action          string  `json:"action"`
	Value           *string `json:"value,omitempty"`
	ExpectedVersion *int    `json:"expectedVersion,omitempty"`
}

type GroupWriteInput struct {
	ProjectID   string        `json:"projectId"`
	Environment string        `json:"environment"`
	Path        string        `json:"path"`
	Changes     []GroupChange `json:"changes"`
	PlanOnly    bool          `json:"planOnly"`
	Confirm     string        `json:"confirm,omitempty"`
}

type GroupWriteItem struct {
	Name    string `json:"name"`
	Action  string `json:"action"`
	Exists  bool   `json:"exists"`
	Version int    `json:"version,omitempty"`
	Status  string `json:"status"`
}

type GroupWriteResult struct {
	ProjectID   string           `json:"projectId"`
	Environment string           `json:"environment"`
	Path        string           `json:"path"`
	Items       []GroupWriteItem `json:"items"`
	Partial     bool             `json:"partial,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// GroupWrite is sequential and intentionally reports partial application.
// A private cross-process lock serializes MCP sessions on this Mac. The
// upstream API has no version precondition, so an external writer can still
// race between the metadata read and the write.
func (s CommandService) GroupWrite(ctx context.Context, input GroupWriteInput) (GroupWriteResult, error) {
	result := GroupWriteResult{ProjectID: input.ProjectID, Environment: input.Environment, Path: input.Path, Items: []GroupWriteItem{}}
	if !resourceSegment.MatchString(input.ProjectID) || !resourceSegment.MatchString(input.Environment) || !validCommandPath(input.Path) || len(input.Changes) == 0 || len(input.Changes) > 100 {
		return result, ErrInvalidCommand
	}
	if !input.PlanOnly && input.Confirm != "secret.batch:"+input.ProjectID+"/"+input.Environment+input.Path {
		return result, ErrConfirmation
	}
	seen := map[string]bool{}
	for _, change := range input.Changes {
		if !resourceSegment.MatchString(change.Name) || seen[change.Name] || (change.Action != "create" && change.Action != "update" && change.Action != "delete") {
			return result, ErrInvalidCommand
		}
		seen[change.Name] = true
		if !input.PlanOnly && change.Action != "delete" && change.Value == nil {
			return result, ErrInvalidCommand
		}
		if change.Action == "delete" && change.Value != nil {
			return result, ErrInvalidCommand
		}
		if !input.PlanOnly && change.Action != "create" && change.ExpectedVersion == nil {
			return result, ErrInvalidCommand
		}
		dummy := ""
		command := Command{Operation: "secret." + change.Action, ProjectID: input.ProjectID, Environment: input.Environment, Path: input.Path, Name: change.Name, Value: &dummy}
		command.Confirm = command.Operation + ":" + input.ProjectID + "/" + input.Environment + input.Path + "/" + change.Name
		if _, _, _, _, err := s.authorizeCommand(ctx, command); err != nil {
			return result, err
		}
	}
	unlock, err := s.lockGroup(input)
	if err != nil {
		return result, err
	}
	defer unlock()
	for _, change := range input.Changes {
		version, exists, err := s.secretVersion(ctx, input, change.Name)
		if err != nil {
			return result, err
		}
		item := GroupWriteItem{Name: change.Name, Action: change.Action, Exists: exists, Version: version, Status: "planned"}
		result.Items = append(result.Items, item)
		if input.PlanOnly {
			continue
		}
		if (change.Action == "create" && exists) || (change.Action != "create" && (!exists || version != *change.ExpectedVersion)) {
			result.Items[len(result.Items)-1].Status = "version_conflict"
			result.Partial = len(result.Items) > 1
			return result, ErrVersionConflict
		}
		command := Command{Operation: "secret." + change.Action, ProjectID: input.ProjectID, Environment: input.Environment, Path: input.Path, Name: change.Name, Value: change.Value}
		command.Confirm = command.Operation + ":" + input.ProjectID + "/" + input.Environment + input.Path + "/" + change.Name
		if _, err := s.Execute(ctx, command); err != nil {
			result.Items[len(result.Items)-1].Status = "unknown_after_request"
			result.Partial = len(result.Items) > 1
			return result, err
		}
		result.Items[len(result.Items)-1].Status = "applied"
	}
	return result, nil
}

func (s CommandService) secretVersion(ctx context.Context, input GroupWriteInput, name string) (int, bool, error) {
	query := url.Values{"projectId": {input.ProjectID}, "environment": {input.Environment}, "secretPath": {input.Path}, "viewSecretValue": {"false"}}
	body, err := s.Client.get(ctx, "/api/v4/secrets/"+name, query)
	if err != nil {
		var api *APIError
		if errors.As(err, &api) && api.StatusCode == http.StatusNotFound {
			return 0, false, nil
		}
		return 0, false, err
	}
	var response struct {
		Secret struct {
			Key     string `json:"secretKey"`
			Version int    `json:"version"`
			Path    string `json:"secretPath"`
		} `json:"secret"`
	}
	if json.Unmarshal(body, &response) != nil || response.Secret.Key != name || response.Secret.Version < 1 || (response.Secret.Path != "" && response.Secret.Path != input.Path) {
		return 0, false, ErrInvalidResponse
	}
	return response.Secret.Version, true, nil
}

func (s CommandService) lockGroup(input GroupWriteInput) (func(), error) {
	if s.ProvisionDir == "" {
		return nil, ErrInvalidCommand
	}
	dir := filepath.Join(s.ProvisionDir, "batch-locks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, ErrProvisionPending
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrProvisionPending
	}
	key := sha256.Sum256([]byte(input.ProjectID + "\x00" + input.Environment + "\x00" + input.Path))
	f, err := os.OpenFile(filepath.Join(dir, hex.EncodeToString(key[:])+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, ErrProvisionPending
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, ErrProvisionPending
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
}
