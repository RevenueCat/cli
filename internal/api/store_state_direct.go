package api

import (
	"context"
	"net/http"
)

// StoreStateService covers the direct per-product store-state endpoints
// (POST /products/{id}/store_state and friends), as opposed to the
// plan/review/apply lifecycle in StoreStatePlansService.
type StoreStateService struct {
	c *Client
}

// ScreenshotUploadReservation is the response of the screenshot_upload
// endpoint: an App Store Connect slot plus presigned upload operations the
// client executes directly against Apple.
type ScreenshotUploadReservation struct {
	ScreenshotID     string            `json:"screenshot_id"`
	Filename         *string           `json:"filename"`
	FileSize         *int64            `json:"file_size"`
	UploadOperations []UploadOperation `json:"upload_operations"`
}

type UploadOperation struct {
	Method         string         `json:"method"`
	URL            string         `json:"url"`
	Length         int64          `json:"length"`
	Offset         int64          `json:"offset"`
	RequestHeaders []UploadHeader `json:"request_headers"`
}

type UploadHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type StoreStateOperationQueued struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
	PollingURL  string `json:"polling_url"`
}

type StoreStateOperation struct {
	ID           string  `json:"id"`
	ProductID    string  `json:"product_id"`
	Status       string  `json:"status"`
	ErrorMessage *string `json:"error_message"`
	CompletedAt  *Millis `json:"completed_at"`
}

// LiveStoreState is the live read of a product's state in its store. Pricing
// is the next effective price per territory (scheduled changes carry
// start_date).
type LiveStoreState struct {
	ProjectID   string            `json:"project_id"`
	ProductID   string            `json:"product_id"`
	Store       string            `json:"store"`
	StoreStatus *StoreStatus      `json:"store_status"`
	Common      *StoreStateCommon `json:"common"`
	StoreState  map[string]any    `json:"store_state"`
	// Warnings is a live-only field: human-readable notes about what's missing
	// or needs attention (e.g. incomplete territory pricing). Empty when clean.
	Warnings []string `json:"warnings"`
}

type StoreStatus struct {
	// Status is RevenueCat's normalized health: ok | needs_action | not_found.
	Status string `json:"status"`
	// RawStoreStatus is the store's own state passed through verbatim (e.g. the
	// App Store's MISSING_METADATA, READY_TO_SUBMIT, APPROVED); nil when the
	// product has no store presence yet (e.g. Test Store).
	RawStoreStatus *string `json:"raw_store_status"`
}

type StoreStateCommon struct {
	Pricing *struct {
		TerritoryPrices map[string]TerritoryPrice `json:"territory_prices"`
	} `json:"pricing"`
	Availability *struct {
		Territories map[string]bool `json:"territories"`
	} `json:"availability"`
	Localizations map[string]struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
	} `json:"localizations"`
}

type TerritoryPrice struct {
	AmountMicros int64   `json:"amount_micros"`
	Currency     string  `json:"currency"`
	StartDate    *string `json:"start_date"`
}

func (s *StoreStateService) Get(ctx context.Context, projectID, productID string) (*LiveStoreState, error) {
	var out LiveStoreState
	err := s.c.do(ctx, http.MethodGet, pathProductStoreState(projectID, productID), nil, &out)
	return &out, err
}

func (s *StoreStateService) ReserveScreenshotUpload(ctx context.Context, projectID, productID, filename string, fileSize int64) (*ScreenshotUploadReservation, error) {
	var out ScreenshotUploadReservation
	body := map[string]any{"filename": filename, "file_size": fileSize}
	err := s.c.do(ctx, http.MethodPost, pathProductStoreStateScreenshotUpload(projectID, productID), body, &out)
	return &out, err
}

// Set enqueues a partial store-state update for a product; omitted fields
// leave App Store Connect unchanged.
func (s *StoreStateService) Set(ctx context.Context, projectID, productID string, body map[string]any) (*StoreStateOperationQueued, error) {
	var out StoreStateOperationQueued
	err := s.c.do(ctx, http.MethodPost, pathProductStoreState(projectID, productID), body, &out)
	return &out, err
}

func (s *StoreStateService) GetOperation(ctx context.Context, projectID, productID, operationID string) (*StoreStateOperation, error) {
	var out StoreStateOperation
	err := s.c.do(ctx, http.MethodGet, pathProductStoreStateOperation(projectID, productID, operationID), nil, &out)
	return &out, err
}
