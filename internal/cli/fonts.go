package cli

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/revenuecat/cli/internal/api"
)

var fontContentTypes = map[string]string{
	".ttf": "font/ttf",
	".otf": "font/otf",
}

const fontMaxBytes = 5 << 20

var errFontTooLarge = errors.New("the upload limit is 5 MiB")

func loadFont(path string) (api.FontCreate, error) {
	contentType, ok := fontContentTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return api.FontCreate{}, fmt.Errorf("unsupported font type %q (accepted: ttf, otf)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return api.FontCreate{}, err
	}
	if len(data) == 0 {
		return api.FontCreate{}, fmt.Errorf("font %s is empty", path)
	}
	if len(data) > fontMaxBytes {
		return api.FontCreate{}, fmt.Errorf("font %s is %d KB: %w", path, len(data)/1024, errFontTooLarge)
	}
	return api.FontCreate{
		Filename:       filepath.Base(path),
		ContentType:    contentType,
		FileDataBase64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func newFontsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fonts",
		Short: "Manage project fonts",
	}
	cmd.AddCommand(newFontsUploadCmd())
	return cmd
}

func newFontsUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <file>",
		Short: "Upload a font to the project for use in paywalls",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt := RuntimeFrom(cmd.Context())
			projectID, err := requireProject(rt)
			if err != nil {
				return err
			}
			body, err := loadFont(args[0])
			if err != nil {
				if errors.Is(err, errFontTooLarge) {
					rt.Out.Hint("Use a font file of 5 MiB or less, then retry")
				}
				return err
			}
			client, err := rt.API()
			if err != nil {
				return err
			}
			font, err := client.Fonts.Create(cmd.Context(), projectID, body)
			if err != nil {
				return err
			}
			rt.Out.Success(fmt.Sprintf("Uploaded %s (%s, %s %d)", font.ID, font.Name, font.Style, font.Weight))
			rt.Out.Hint("Reference it from paywall components as " + font.FontKey)
			return rt.Out.Render(font)
		},
	}
}
