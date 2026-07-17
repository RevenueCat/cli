# PR #5 Review: Self-updater and Xcode-free install

Reviewed commits on branch `pr-05-update-install` against base `pr-04-release-tooling`.

## Summary

This PR adds two major features:
1. **Self-updater** (`rc update`) - downloads and installs the latest release from GitHub
2. **Xcode-free install** (`install.sh`) - shell script for direct installation without Homebrew/Xcode

## Focus Areas Reviewed

✅ **Correctness bugs**  
✅ **Auth/credential handling**  
✅ **CLI agent contract compliance** (--json stability, --no-input behavior)

---

## ✅ Strengths

### 1. **Excellent CLI Agent Contract Compliance**

**JSON Output Stability:**
- All `rc update` paths emit consistent schema: `installed_version`, `latest_version`, `up_to_date`, `updated`
- Dev builds add `development_build: true`
- Test coverage: `TestUpdate_ConsistentJSONSchema` validates all four keys are present
- The `SilentExitError` mechanism (lines 85-95 in update.go) correctly handles `--check --json` to emit exactly one JSON document to stdout and exit 1 with no stderr output

**--no-input Handling:**
- Uses `tui.Confirm(rt.Globals.NoInput, ...)` which properly rejects prompts under `--no-input`
- Error message clearly states: "pass --yes to confirm non-interactively"
- Consistent with the rest of the codebase (same pattern in packages, webhooks, customers, etc.)

### 2. **Security: Excellent Tar Extraction Protection in Go Updater**

The `downloadTarGz` function (updater.go:192-260) has exemplary path traversal protection:

```go
// Line 234-236: Intentionally avoids filepath.Clean to prevent bypass
if hdr.Name == "rc" && hdr.Typeflag == tar.TypeReg {
```

**Why this is good:**
- Requires exact match of "rc" with no path components (rejects "bin/rc", "./rc", "bin/../rc")
- Checks `tar.TypeReg` to reject symlinks, directories, and other non-regular files
- Test coverage: `TestDownloadBinary_RejectsNestedPath` and `TestDownloadBinary_RejectsTraversalBypass`

### 3. **Atomic Binary Replacement**

The `Install` function (updater.go:95-138) uses proper atomic replacement:
- Creates temp file in the same directory as destination (ensures same filesystem)
- Uses `os.Rename()` which is atomic on POSIX systems
- Proper cleanup with deferred removal on failure paths
- Test coverage: `TestInstall_CleansUpTempOnFailure`

### 4. **No Credential Leakage**

The update command correctly avoids touching any credentials:
- No API key required
- No interaction with RevenueCat API
- Only hits GitHub's public releases API

### 5. **install.sh: Portable & Secure Argument Handling**

```sh
# Line 117: Portable mktemp (works on macOS/Linux)
TMP="$(mktemp -d "${TMPDIR:-/tmp}/rc.XXXXXX")"

# Lines 153-154: Proper sudo invocation with -- separator prevents option injection
sudo mkdir -p -- "$INSTALL_DIR"
sudo mv -- "$EXTRACTED" "$DEST"
```

The `--` separator prevents malicious `--install-dir` values like `--help` or `-rf` from being interpreted as options.

---

## 🔍 Findings

### 1. 🟡 **install.sh: Missing Symlink Protection** (Medium Severity)

**Location:** `install.sh` lines 131-135

**Issue:**  
The shell script doesn't verify that the extracted binary is a regular file, not a symlink. While the Go updater (`rc update`) correctly rejects symlinks via `tar.TypeReg` check, the install script does not.

**Attack Scenario:**  
If an attacker compromises GitHub release assets and replaces the archive with one containing "rc" as a symlink to a malicious file, the install script would:
1. Extract the symlink to `$TMP/rc`
2. Pass the `[ ! -f "$EXTRACTED" ]` check (if the symlink target exists)
3. Move the symlink to `/usr/local/bin/rc`
4. Users would execute the attacker's binary

**Current code:**
```sh
if [ ! -f "$EXTRACTED" ]; then
  echo "Binary not found after extraction." >&2
  exit 1
fi
```

**Recommended fix:**
```sh
if [ ! -f "$EXTRACTED" ] || [ -L "$EXTRACTED" ]; then
  echo "Binary not found or is a symbolic link." >&2
  exit 1
fi
```

**Severity Justification:**  
Medium (not high) because:
- Attack requires compromising GitHub's release infrastructure (already a severe breach)
- Attacker could just replace the entire binary if they have that access
- The Go updater is already protected
- Defense-in-depth improvement rather than critical vulnerability

---

### 2. ✅ **Minor: Error Handling for Close() in downloadTarGz** (Verified Correct)

Initially flagged for review but confirmed correct on deeper inspection:

```go
// Line 249-252: Properly handles Close() failure
if err := tmp.Close(); err != nil {
    os.Remove(tmpName)
    return "", fmt.Errorf("flushing extracted binary: %w", err)
}
```

All error paths correctly close `tmp` before removing `tmpName`. ✅

---

### 3. ✅ **Dev Build Version Check** (Acceptable)

**Location:** `update.go` line 40

```go
if currentVersion == "dev" {
```

Simple string comparison. Edge case: versions like "dev-123" or "development" won't be caught, but:
- "dev" is the conventional value for development builds (set in Makefile/build scripts)
- Worst case: attempts to update a dev build and fails gracefully
- Not a correctness bug ✅

---

### 4. ✅ **No Retry Logic for GitHub API** (Acceptable)

The updater uses a simple 30-second timeout without retry logic for transient failures:

```go
hc := &http.Client{Timeout: 30 * time.Second}
```

**Why this is acceptable:**
- Update is a user-initiated action (not automated)
- User can simply retry if it fails
- Keeps code simple
- GitHub's API is generally reliable

---

## 📋 Test Coverage Review

**Excellent test coverage:**

### CLI Tests (`internal/cli/update_test.go`)
- ✅ Dev build JSON output with `development_build` flag
- ✅ Up-to-date check in JSON mode
- ✅ `--check --json` exits 1 with single JSON document on stdout, nothing on stderr
- ✅ `--check` human mode exits 1 with nothing on stdout
- ✅ Consistent JSON schema across all paths

### Updater Tests (`internal/updater/updater_test.go`)
- ✅ Semver comparison including pre-release handling
- ✅ FetchRelease success and error paths
- ✅ DownloadBinary success, Windows rejection, missing asset
- ✅ **Security:** Path traversal rejection ("bin/rc", "bin/../rc")
- ✅ Install atomicity, permissions, cleanup on failure

**Test quality:** High. Edge cases are well covered.

---

## 🏗️ Architecture Review

**Follows AGENTS.md conventions:**

### Two-Layer Separation ✅
- `internal/updater/` - pure logic, no CLI concepts
- `internal/cli/update.go` - CLI layer, uses updater package

### Dual-Mode Contract ✅
- stdout = data (JSON output)
- stderr = chatter (progress messages, hints)
- `--json` never writes to stderr ✅
- `--no-input` requires `--yes` for confirmation ✅

### No Globals ✅
- State flows through `cli.Runtime` on `cmd.Context()`
- Test override via `RC_UPDATER_RELEASES_URL` env var

---

## 📝 Code Quality

**Strong points:**
- Clear variable names (`latestVersion`, `currentVersion`, `tmpPath`, `execPath`)
- Minimal comments (code is self-explanatory)
- Proper error wrapping with `%w`
- Consistent error messages
- No unnecessary abstractions

**Formatting:** ✅ `gofmt` clean  
**Vet:** ✅ No issues  
**Tests:** ✅ All passing

---

## 🎯 Recommendations

### Required
1. **Fix install.sh symlink check** (see Finding #1)

### Optional
None - the rest of the implementation is solid.

---

## Final Verdict

**✅ APPROVE with one fix required**

The implementation is high-quality with excellent security practices, proper agent contract compliance, and comprehensive test coverage. The only issue requiring a fix is the missing symlink check in `install.sh`, which is a defense-in-depth improvement.

**After applying the symlink fix, this PR is ready to merge.**

---

## Files Reviewed

- ✅ `internal/cli/update.go` (147 lines)
- ✅ `internal/updater/updater.go` (261 lines)
- ✅ `install.sh` (166 lines)
- ✅ `internal/cli/update_test.go` (182 lines)
- ✅ `internal/updater/updater_test.go` (283 lines)
- ✅ Modified: `README.md`, `docs/command-surface.md`, `internal/cli/root.go`, `internal/cli/run.go`, `internal/cli/runtime.go`

**Total lines reviewed:** ~1,000+ lines of new/changed code
