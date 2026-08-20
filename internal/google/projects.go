package google

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v3"
	"google.golang.org/api/option"
)

// Project is a Google Cloud project the signed-in user can access.
type Project struct {
	ID          string // e.g. "my-app-production"
	DisplayName string // human name, may be empty
}

// ListProjects returns the active Cloud projects accessible to the token. It
// uses projects.search, which needs no parent and returns everything the caller
// can see, so the developer never has to copy a project ID by hand.
func ListProjects(ctx context.Context, ts oauth2.TokenSource) ([]Project, error) {
	svc, err := cloudresourcemanager.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("cloud resource manager client: %w", err)
	}
	var projects []Project
	call := svc.Projects.Search()
	err = call.Pages(ctx, func(page *cloudresourcemanager.SearchProjectsResponse) error {
		for _, p := range page.Projects {
			if p.State != "ACTIVE" {
				continue
			}
			projects = append(projects, Project{ID: p.ProjectId, DisplayName: p.DisplayName})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("search projects: %w", err)
	}
	return projects, nil
}
