package infisical

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
)

var (
	projectSlug           = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,126}[a-z0-9])?$`)
	ErrInvalidProjectSlug = errors.New("invalid project slug")
)

// ProjectAccess describes what the current machine identity can see. An empty
// project list is scoped to this identity; it does not mean no projects exist.
type ProjectAccess struct {
	Slug         string `json:"slug"`
	Listed       bool   `json:"listed"`
	DetailStatus string `json:"detailStatus"`
	ProjectID    string `json:"projectId,omitempty"`
}

// ListProjectIDs returns only stable project IDs visible to the current
// machine identity. Callers use this as one side of an authorization
// intersection; it does not grant access to any project.
func (c *Client) ListProjectIDs(ctx context.Context) (map[string]bool, error) {
	body, err := c.get(ctx, "/api/v1/projects", nil)
	if err != nil {
		return nil, err
	}
	var listing struct {
		Projects *[]struct {
			ID string `json:"id"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || listing.Projects == nil {
		return nil, ErrInvalidResponse
	}
	ids := make(map[string]bool, len(*listing.Projects))
	for _, project := range *listing.Projects {
		if !resourceSegment.MatchString(project.ID) || ids[project.ID] {
			return nil, ErrInvalidResponse
		}
		ids[project.ID] = true
	}
	return ids, nil
}

// InspectProjectAccess uses only project metadata reads. Infisical v0.151.0
// lists identity projects through explicit project memberships, independently
// of the identity's organization role.
func (c *Client) InspectProjectAccess(ctx context.Context, slug string) (ProjectAccess, error) {
	if !projectSlug.MatchString(slug) {
		return ProjectAccess{}, ErrInvalidProjectSlug
	}
	body, err := c.get(ctx, "/api/v1/projects", nil)
	if err != nil {
		return ProjectAccess{}, err
	}
	var listing struct {
		Projects *[]struct {
			Slug string `json:"slug"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &listing); err != nil || listing.Projects == nil {
		return ProjectAccess{}, ErrInvalidResponse
	}
	result := ProjectAccess{Slug: slug}
	for _, project := range *listing.Projects {
		if project.Slug == slug {
			result.Listed = true
			break
		}
	}
	body, err = c.get(ctx, "/api/v1/projects/slug/"+slug, nil)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case http.StatusForbidden:
				result.DetailStatus = "forbidden"
				return result, nil
			case http.StatusNotFound:
				result.DetailStatus = "not_found"
				return result, nil
			}
		}
		return ProjectAccess{}, err
	}
	var detail struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(body, &detail); err != nil || detail.ID == "" || detail.Slug != slug {
		return ProjectAccess{}, ErrInvalidResponse
	}
	result.DetailStatus = "allowed"
	result.ProjectID = detail.ID
	return result, nil
}
