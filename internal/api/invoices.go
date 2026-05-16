package api

import (
	"context"
	"net/http"
)

type InvoicesService struct{ c *Client }

type Invoice struct {
	ID         string `json:"id"`
	CustomerID string `json:"customer_id,omitempty"`
	IssuedAt   Millis `json:"issued_at,omitempty"`
	AmountUSD  any    `json:"amount_in_usd,omitempty"`
	Status     string `json:"status,omitempty"`
	Object     string `json:"object,omitempty"`
}

// GET /projects/{project_id}/invoices/{id}
func (s *InvoicesService) Get(ctx context.Context, projectID, id string) (*Invoice, error) {
	var out Invoice
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "invoices", id), nil, &out)
	return &out, err
}

// GET /projects/{project_id}/customers/{customer_id}/invoices
//
// Lives in the InvoicesService rather than CustomersService because the
// invoice resource is the noun being listed; the customer is just the filter.
func (s *InvoicesService) ListForCustomer(ctx context.Context, projectID, customerID string) (*Page[Invoice], error) {
	var out Page[Invoice]
	err := s.c.do(ctx, http.MethodGet, encodePath("projects", projectID, "customers", customerID, "invoices"), nil, &out)
	return &out, err
}
