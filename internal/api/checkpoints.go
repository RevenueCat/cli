package api

import (
	"context"
	"net/http"
)

// CheckpointsService wraps /checkpoints, the points in an app where
// Checkpoint rules decide which Flow to show.
type CheckpointsService struct{ c *Client }

type Checkpoint struct {
	ID         string  `json:"id"`
	Object     string  `json:"object,omitempty"`
	ProjectID  string  `json:"project_id,omitempty"`
	Identifier string  `json:"identifier"`
	CreatedVia string  `json:"created_via,omitempty"`
	CreatedAt  Millis  `json:"created_at,omitempty"`
	UpdatedAt  *Millis `json:"updated_at,omitempty"`
}

func (s *CheckpointsService) List(ctx context.Context, projectID string, opts *ListOptions) (*Page[Checkpoint], error) {
	var out Page[Checkpoint]
	if err := s.c.do(ctx, http.MethodGet, pathCheckpoints(projectID)+opts.query(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *CheckpointsService) Create(ctx context.Context, projectID string, body CreateCheckpointRequest) (*Checkpoint, error) {
	var out Checkpoint
	if err := s.c.do(ctx, http.MethodPost, pathCheckpoints(projectID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
