package google

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

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

var (
	projectIDInvalid = regexp.MustCompile(`[^a-z0-9-]+`)
	projectIDDashes  = regexp.MustCompile(`-+`)
)

// ProjectIDFromName derives a valid, likely-unique Google Cloud project ID from
// a display name. Project IDs must be 6–30 chars, start with a lowercase
// letter, and contain only lowercase letters, digits, and hyphens. A short
// random suffix keeps it from colliding with an existing global ID.
func ProjectIDFromName(name string) string {
	slug := projectIDInvalid.ReplaceAllString(strings.ToLower(name), "-")
	slug = projectIDDashes.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" || slug[0] < 'a' || slug[0] > 'z' {
		slug = "rc-" + slug
		slug = strings.Trim(slug, "-")
	}
	const suffixLen = 6
	if len(slug) > 30-suffixLen-1 {
		slug = strings.TrimRight(slug[:30-suffixLen-1], "-")
	}
	return slug + "-" + randomSuffix(suffixLen)
}

// CreateProject creates a new Google Cloud project and waits for it to become
// active. Project creation is a long-running operation, so this polls until it
// completes. The signed-in user needs permission to create projects (personal
// accounts can, subject to quota; org accounts may require a folder/org parent
// their admin controls).
func CreateProject(ctx context.Context, ts oauth2.TokenSource, projectID, displayName string) error {
	svc, err := cloudresourcemanager.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return fmt.Errorf("cloud resource manager client: %w", err)
	}
	op, err := svc.Projects.Create(&cloudresourcemanager.Project{
		ProjectId:   projectID,
		DisplayName: displayName,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("create project %s: %w", projectID, classifyOrgPolicy(err))
	}
	// Poll the long-running operation until the project is ready.
	for i := 0; i < 60; i++ {
		if op.Done {
			if op.Error != nil {
				return fmt.Errorf("create project %s: %s", projectID, op.Error.Message)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		op, err = svc.Operations.Get(op.Name).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("wait for project %s: %w", projectID, err)
		}
	}
	return errors.New("timed out waiting for the new project to become ready")
}
