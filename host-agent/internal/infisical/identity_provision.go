package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

type identityReceipt struct {
	Name           string `json:"name"`
	OrganizationID string `json:"organizationId"`
	ID             string `json:"id,omitempty"`
}

func (s CommandService) identityReceiptPath(name string) (string, error) {
	if s.ProvisionDir == "" {
		return "", ErrProjectDenied
	}
	if err := os.MkdirAll(s.ProvisionDir, 0700); err != nil {
		return "", ErrProvisionPending
	}
	info, err := os.Lstat(s.ProvisionDir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", ErrProvisionPending
	}
	return filepath.Join(s.ProvisionDir, "identity-"+name+".json"), nil
}

func (s CommandService) createIdentity(ctx context.Context, command Command, result CommandResult) (CommandResult, error) {
	path, err := s.identityReceiptPath(command.Name)
	if err != nil {
		return CommandResult{}, err
	}
	marker, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return CommandResult{}, ErrProvisionPending
		}
		var receipt identityReceipt
		if json.Unmarshal(data, &receipt) != nil || receipt.Name != command.Name || receipt.OrganizationID != command.OrganizationID {
			return CommandResult{}, ErrInvalidCommand
		}
		if receipt.ID == "" {
			return CommandResult{}, ErrProvisionPending
		}
		result.Applied = true
		result.Items = []Resource{{ID: receipt.ID, Name: receipt.Name}}
		return result, nil
	}
	if err != nil {
		return CommandResult{}, ErrProvisionPending
	}
	initial, _ := json.Marshal(identityReceipt{Name: command.Name, OrganizationID: command.OrganizationID})
	_, writeErr := marker.Write(initial)
	syncErr := marker.Sync()
	closeErr := marker.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return CommandResult{}, ErrProvisionPending
	}
	body, err := s.Client.request(ctx, http.MethodPost, "/api/v1/identities", nil, map[string]any{"name": command.Name, "organizationId": command.OrganizationID, "role": "no-access", "hasDeleteProtection": false})
	if err != nil {
		return CommandResult{}, err
	}
	var created struct {
		Identity struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"identity"`
	}
	if json.Unmarshal(body, &created) != nil || !resourceSegment.MatchString(created.Identity.ID) || created.Identity.Name != command.Name {
		return CommandResult{}, ErrInvalidResponse
	}
	encoded, _ := json.Marshal(identityReceipt{Name: command.Name, OrganizationID: command.OrganizationID, ID: created.Identity.ID})
	temp := path + ".tmp"
	if err := os.WriteFile(temp, encoded, 0600); err != nil {
		return CommandResult{}, ErrProvisionPending
	}
	if err := os.Rename(temp, path); err != nil {
		return CommandResult{}, ErrProvisionPending
	}
	result.Applied = true
	result.Items = []Resource{{ID: created.Identity.ID, Name: created.Identity.Name}}
	return result, nil
}

func (s CommandService) deleteIdentity(ctx context.Context, command Command, result CommandResult) (CommandResult, error) {
	path, err := s.identityReceiptPath(command.Name)
	if err != nil {
		return CommandResult{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CommandResult{}, ErrProjectDenied
	}
	var receipt identityReceipt
	if json.Unmarshal(data, &receipt) != nil || receipt.Name != command.Name || receipt.ID != command.IdentityID || receipt.OrganizationID != command.OrganizationID {
		return CommandResult{}, ErrProjectDenied
	}
	detail, err := s.Client.get(ctx, "/api/v1/identities/"+command.IdentityID, nil)
	if err != nil {
		return CommandResult{}, err
	}
	var item struct {
		Identity struct {
			OrganizationID string `json:"orgId"`
			Identity       struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			} `json:"identity"`
		} `json:"identity"`
	}
	if json.Unmarshal(detail, &item) != nil || item.Identity.Identity.Name != command.Name || item.Identity.Identity.ID != command.IdentityID || item.Identity.OrganizationID != command.OrganizationID {
		return CommandResult{}, ErrProjectDenied
	}
	if _, err := s.Client.request(ctx, http.MethodDelete, "/api/v1/identities/"+command.IdentityID, nil, nil); err != nil {
		return CommandResult{}, err
	}
	if err := os.Remove(path); err != nil {
		return CommandResult{}, ErrProvisionPending
	}
	result.Applied = true
	return result, nil
}
