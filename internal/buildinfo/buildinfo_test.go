package buildinfo

import "testing"

func TestIsDev(t *testing.T) {
	orig := Version
	defer func() { Version = orig }()

	Version = "dev"
	if !IsDev() {
		t.Error(`Version "dev" should be IsDev()`)
	}
	Version = "0.1.0"
	if IsDev() {
		t.Error("a release version should not be IsDev()")
	}
}
