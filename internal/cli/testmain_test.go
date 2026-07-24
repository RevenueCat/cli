package cli

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// Use an in-memory keyring for the whole package so commands that call
// config.Load/Save during tests never touch the real OS keychain — otherwise a
// developer's actual `rc login` leaks in and flips logged-out assertions
// (e.g. the auth-status snapshot) to logged-in. Headless CI has no keyring, so
// this only bites locally; the mock makes tests deterministic everywhere.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
