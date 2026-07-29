//go:build !darwin || !cgo

package cli

import "fmt"

func saveRevenueCatPasswordToKeychain(_, _ string) error {
	return fmt.Errorf("saving website passwords requires a native macOS build with Keychain support")
}
