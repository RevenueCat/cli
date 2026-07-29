package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newAPICmd() *cobra.Command {
	var bodyFlag string
	cmd := &cobra.Command{
		Use:   "api <method> <path>",
		Short: "Make a raw API request to RevenueCat v2",
		Long: `Send a raw HTTP request to the RevenueCat v2 API. Auth headers are set
automatically from your active profile.

<method> is the HTTP verb: GET, POST, PUT, DELETE, PATCH.
<path> is relative to https://api.revenuecat.com/v2, e.g.
  /projects/proj_abc/customers

Pass --body with a JSON string. Use --body=@- to read from stdin,
or --body=@filename to read from a file.

Exit code reflects the HTTP status: non-2xx responses exit non-zero.`,
		Example: `  rc api GET /projects/proj_abc/customers
  rc api GET /projects/proj_abc/customers?limit=10
  rc api POST /projects/proj_abc/offerings --body '{"lookup_key":"sale","display_name":"Sale"}'
  echo '{"lookup_key":"sale"}' | rc api POST /projects/proj_abc/offerings --body @-
  rc api DELETE /projects/proj_abc/offerings/ofrng_xyz`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			client, err := rt.API()
			if err != nil {
				return err
			}
			method := strings.ToUpper(args[0])
			path := args[1]

			var body []byte
			if bodyFlag != "" {
				switch {
				case bodyFlag == "@-":
					body, err = io.ReadAll(os.Stdin)
					if err != nil {
						return fmt.Errorf("reading stdin: %w", err)
					}
				case strings.HasPrefix(bodyFlag, "@"):
					body, err = os.ReadFile(bodyFlag[1:])
					if err != nil {
						return fmt.Errorf("reading file: %w", err)
					}
				default:
					body = []byte(bodyFlag)
				}
			}

			data, status, err := client.Raw(cmd.Context(), method, path, body)
			if err != nil {
				return err
			}
			if len(data) > 0 {
				if _, werr := cmd.OutOrStdout().Write(data); werr != nil {
					return werr
				}
				// Ensure trailing newline for shell friendliness.
				if len(data) > 0 && data[len(data)-1] != '\n' {
					_, _ = fmt.Fprintln(cmd.OutOrStdout())
				}
			}
			if status < 200 || status >= 300 {
				var code int
				switch {
				case status == 401 || status == 403:
					code = 4
				case status == 404:
					code = 5
				case status == 429:
					code = 6
				default:
					code = 1
				}
				return &SilentExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&bodyFlag, "body", "", `request body: JSON string, @file, or @- for stdin`)
	return cmd
}
