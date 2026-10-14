package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

var ErrProvisionPending = errors.New("project provision outcome needs reconciliation")

type provisionReceipt struct {
	Name              string `json:"name"`
	Slug              string `json:"slug"`
	Description       string `json:"description"`
	Template          string `json:"template"`
	CreateDefaultEnvs bool   `json:"createDefaultEnvs"`
	ProjectID         string `json:"projectId,omitempty"`
	Complete          bool   `json:"complete"`
}

// createProject uses a durable request marker. A lost POST response remains
// pending for operator reconciliation rather than creating a second project.
func (s CommandService) createProject(ctx context.Context, command Command, result CommandResult) (CommandResult, error) {
	defaultEnvs := true
	if command.CreateDefaultEnvs != nil {
		defaultEnvs = *command.CreateDefaultEnvs
	}
	expected := provisionReceipt{Name: command.Name, Slug: command.Slug, Description: command.Description, Template: command.Template, CreateDefaultEnvs: defaultEnvs}
	var receiptPath string
	if s.MCPPolicy != nil {
		if command.RequestID == "" || s.ProvisionDir == "" {
			return CommandResult{}, ErrInvalidCommand
		}
		if err := os.MkdirAll(s.ProvisionDir, 0700); err != nil {
			return CommandResult{}, ErrProvisionPending
		}
		info, err := os.Lstat(s.ProvisionDir)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return CommandResult{}, ErrProvisionPending
		}
		receiptPath = filepath.Join(s.ProvisionDir, command.RequestID+".json")
		marker, err := os.OpenFile(receiptPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			encoded, _ := json.Marshal(expected)
			_, writeErr := marker.Write(encoded)
			syncErr := marker.Sync()
			closeErr := marker.Close()
			if writeErr != nil || syncErr != nil || closeErr != nil {
				return CommandResult{}, ErrProvisionPending
			}
		} else if errors.Is(err, os.ErrExist) {
			stored, readErr := os.ReadFile(receiptPath)
			if readErr != nil {
				return CommandResult{}, ErrProvisionPending
			}
			var prior provisionReceipt
			if json.Unmarshal(stored, &prior) != nil || prior.Name != expected.Name || prior.Slug != expected.Slug || prior.Description != expected.Description || prior.Template != expected.Template || prior.CreateDefaultEnvs != expected.CreateDefaultEnvs {
				return CommandResult{}, ErrInvalidCommand
			}
			if !prior.Complete {
				return CommandResult{}, ErrProvisionPending
			}
			return s.readCreatedProject(ctx, prior.ProjectID, result)
		} else {
			return CommandResult{}, ErrProvisionPending
		}
	}
	fields := map[string]any{"projectName": command.Name, "projectDescription": command.Description, "type": "secret-manager", "shouldCreateDefaultEnvs": defaultEnvs}
	if command.Slug != "" {
		fields["slug"] = command.Slug
	}
	if command.Template != "" {
		fields["template"] = command.Template
	}
	body, err := s.Client.request(ctx, http.MethodPost, "/api/v1/projects", nil, fields)
	if err != nil {
		return CommandResult{}, err
	}
	var response struct {
		Project struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"project"`
	}
	if json.Unmarshal(body, &response) != nil || !resourceSegment.MatchString(response.Project.ID) || response.Project.Name != command.Name || response.Project.Slug == "" || (command.Slug != "" && response.Project.Slug != command.Slug) {
		return CommandResult{}, ErrInvalidResponse
	}
	if !defaultEnvs {
		_, err = s.Client.request(ctx, http.MethodPost, "/api/v1/projects/"+response.Project.ID+"/environments", nil, map[string]string{"name": "Development", "slug": "dev"})
		if err != nil {
			return CommandResult{}, err
		}
	}
	if receiptPath != "" {
		expected.ProjectID, expected.Complete = response.Project.ID, true
		encoded, _ := json.Marshal(expected)
		temp := receiptPath + ".tmp"
		if err := os.WriteFile(temp, encoded, 0600); err != nil {
			return CommandResult{}, ErrProvisionPending
		}
		if err := os.Rename(temp, receiptPath); err != nil {
			return CommandResult{}, ErrProvisionPending
		}
	}
	return s.readCreatedProject(ctx, response.Project.ID, result)
}

func (s CommandService) readCreatedProject(ctx context.Context, id string, result CommandResult) (CommandResult, error) {
	body, err := s.Client.get(ctx, "/api/v1/projects/"+id, nil)
	if err != nil {
		return CommandResult{}, err
	}
	var response struct {
		Project struct {
			ID           string                `json:"id"`
			Name         string                `json:"name"`
			Slug         string                `json:"slug"`
			Description  string                `json:"description"`
			Type         string                `json:"type"`
			Environments []EnvironmentMetadata `json:"environments"`
		} `json:"project"`
	}
	if json.Unmarshal(body, &response) != nil || response.Project.ID != id || response.Project.Name == "" || response.Project.Slug == "" {
		return CommandResult{}, ErrInvalidResponse
	}
	result.Applied = true
	result.Items = []Resource{{ID: id, Name: response.Project.Name, Slug: response.Project.Slug, Description: response.Project.Description, Type: response.Project.Type, Environments: response.Project.Environments}}
	visible, err := s.Client.ListProjectIDs(ctx)
	if err == nil && visible[id] {
		result.EnrollmentStatus = "host_identity_visible"
	} else {
		result.EnrollmentStatus = "pending_reconciliation"
	}
	return result, nil
}
