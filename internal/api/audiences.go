package api

import (
	"context"
	"net/http"
	"net/url"
)

// AudiencesService wraps /audiences. The list endpoint is not paginated: it
// returns every audience in one page.
type AudiencesService struct{ c *Client }

type Audience struct {
	ID             string               `json:"id"`
	Object         string               `json:"object,omitempty"`
	ProjectID      string               `json:"project_id,omitempty"`
	CustomerListID string               `json:"customer_list_id,omitempty"`
	Name           string               `json:"name"`
	Rules          AudienceRules        `json:"rules"`
	CreatedAt      Millis               `json:"created_at,omitempty"`
	UpdatedAt      *Millis              `json:"updated_at,omitempty"`
	Stats          *AudienceStats       `json:"stats,omitempty"`
	CustomerSample []AudienceSampleItem `json:"customer_sample,omitempty"`
	UsedBy         *AudienceUsedBy      `json:"used_by,omitempty"`
}

// AudienceRules: groups are OR'd together, conditions within a group are AND'd.
type AudienceRules struct {
	Groups []AudienceRuleGroup `json:"groups"`
}

type AudienceRuleGroup struct {
	Conditions []AudienceRuleCondition `json:"conditions"`
}

type AudienceRuleCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value,omitempty"`
	Currency string `json:"currency,omitempty"`
}

type AudienceStats struct {
	TotalCustomers      int64   `json:"total_customers"`
	ActiveSubscriptions int64   `json:"active_subscriptions"`
	ActiveTrials        int64   `json:"active_trials"`
	TotalRevenue        float64 `json:"total_revenue"`
	Currency            string  `json:"currency"`
	IsApproximate       bool    `json:"is_approximate"`
}

type AudienceSampleItem struct {
	AppUserID         string   `json:"app_user_id"`
	Email             *string  `json:"email,omitempty"`
	Status            string   `json:"status"`
	TotalSpent        *float64 `json:"total_spent,omitempty"`
	Currency          string   `json:"currency"`
	LastSeenAt        *int64   `json:"last_seen_at,omitempty"`
	LatestProductName *string  `json:"latest_product_name,omitempty"`
}

type AudienceUsedBy struct {
	TargetingRules []AudienceReference `json:"targeting_rules"`
	Experiments    []AudienceReference `json:"experiments"`
}

// AudienceReference is a targeting rule (with State) or an experiment (with
// Status) that uses the audience.
type AudienceReference struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	State       string `json:"state,omitempty"`
	Status      string `json:"status,omitempty"`
}

// AudienceExpand names the optional sections get-audience can embed.
type AudienceExpand string

const (
	AudienceExpandStats          AudienceExpand = "stats"
	AudienceExpandCustomerSample AudienceExpand = "customer_sample"
	AudienceExpandUsedBy         AudienceExpand = "used_by"
)

func (s *AudiencesService) List(ctx context.Context, projectID string) (*Page[Audience], error) {
	var out Page[Audience]
	err := s.c.do(ctx, http.MethodGet, pathAudiences(projectID), nil, &out)
	return &out, err
}

func (s *AudiencesService) Get(ctx context.Context, projectID, id string, expand ...AudienceExpand) (*Audience, error) {
	path := pathAudience(projectID, id)
	if len(expand) > 0 {
		query := url.Values{}
		for _, e := range expand {
			query.Add("expand", string(e))
		}
		path += "?" + query.Encode()
	}
	var out Audience
	err := s.c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}
