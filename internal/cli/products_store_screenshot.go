package cli

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

func newProductsStoreScreenshotCmd() *cobra.Command {
	var file string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "screenshot <product-id>",
		Short: "Upload a real App Review screenshot for a product",
		Long: `Uploads a paywall screenshot to App Store Connect for App Review and
attaches it to the product, replacing the automatic placeholder.

The image bytes upload directly to Apple through presigned URLs; RevenueCat
only stores the resulting screenshot reference. Attaching is asynchronous —
the command waits for the operation to finish.

Apple accepts PNG or JPEG review screenshots.`,
		Example: `  rc products store screenshot prod_abc --file paywall.png
  rc products store screenshot prod_abc --file paywall.png --json --no-input`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			if file == "" {
				return fmt.Errorf("a screenshot image is required: pass --file <path to .png or .jpg>")
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Errorf("read screenshot: %w", err)
			}
			if len(data) == 0 {
				return fmt.Errorf("screenshot %s is empty", file)
			}
			filename := filepath.Base(file)
			sum := md5.Sum(data)
			checksum := hex.EncodeToString(sum[:])
			productID := args[0]

			rt.Out.Info(fmt.Sprintf("Reserving App Store Connect screenshot slot for %s (%d KB)…", filename, len(data)/1024))
			reservation, err := client.StoreState.ReserveScreenshotUpload(cmd.Context(), projectID, productID, filename, int64(len(data)))
			if err != nil {
				return err
			}
			if len(reservation.UploadOperations) == 0 {
				return fmt.Errorf("Apple returned no upload operations for screenshot %s", reservation.ScreenshotID)
			}

			rt.Out.Info(fmt.Sprintf("Uploading to Apple (%d part(s))…", len(reservation.UploadOperations)))
			for i, op := range reservation.UploadOperations {
				if err := executeUploadOperation(cmd.Context(), op, data); err != nil {
					return fmt.Errorf("upload part %d/%d: %w", i+1, len(reservation.UploadOperations), err)
				}
			}

			rt.Out.Info("Attaching the screenshot to the product…")
			queued, err := client.StoreState.Set(cmd.Context(), projectID, productID, map[string]any{
				"store": "app_store",
				"store_state": map[string]any{
					"review_information": map[string]any{
						"screenshot": map[string]any{
							"screenshot_id":        reservation.ScreenshotID,
							"filename":             filename,
							"file_size":            int64(len(data)),
							"source_file_checksum": checksum,
						},
					},
				},
			})
			if err != nil {
				return err
			}

			operation, err := waitForStoreStateOperation(cmd.Context(), client, projectID, productID, queued.OperationID, timeout)
			if err != nil {
				return err
			}
			if operation.Status != "succeeded" {
				message := "operation " + operation.ID + " " + operation.Status
				if operation.ErrorMessage != nil && *operation.ErrorMessage != "" {
					message += ": " + *operation.ErrorMessage
				}
				return fmt.Errorf("attach screenshot: %s", message)
			}
			rt.Out.Success(fmt.Sprintf("Review screenshot %s attached to %s", filename, productID))
			return rt.Out.Render(map[string]any{
				"ok":            true,
				"product_id":    productID,
				"screenshot_id": reservation.ScreenshotID,
				"operation_id":  operation.ID,
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "screenshot image to upload (.png or .jpg)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "how long to wait for the attach operation")
	return cmd
}

// executeUploadOperation performs one presigned upload directly against
// Apple: no RevenueCat auth, exactly the method/URL/headers Apple specified,
// and only the requested byte range of the source file.
func executeUploadOperation(ctx context.Context, op api.UploadOperation, data []byte) error {
	if op.Offset < 0 || op.Length <= 0 || op.Offset+op.Length > int64(len(data)) {
		return fmt.Errorf("upload range %d..%d exceeds file size %d", op.Offset, op.Offset+op.Length, len(data))
	}
	chunk := data[op.Offset : op.Offset+op.Length]
	method := strings.ToUpper(op.Method)
	if method == "" {
		method = http.MethodPut
	}
	req, err := http.NewRequestWithContext(ctx, method, op.URL, bytes.NewReader(chunk))
	if err != nil {
		return err
	}
	req.ContentLength = op.Length
	for _, h := range op.RequestHeaders {
		req.Header.Set(h.Name, h.Value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("Apple upload returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func waitForStoreStateOperation(ctx context.Context, client *api.Client, projectID, productID, operationID string, timeout time.Duration) (*api.StoreStateOperation, error) {
	deadline := time.Now().Add(timeout)
	for {
		operation, err := client.StoreState.GetOperation(ctx, projectID, productID, operationID)
		if err != nil {
			return nil, err
		}
		if operation.Status == "succeeded" || operation.Status == "failed" {
			return operation, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s waiting for operation %s (status %s); check later with the operation ID", timeout, operationID, operation.Status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
