package cli

import (
	"strings"
	"testing"
)

func agentByToolkitKey(t *testing.T, key string) agentClient {
	t.Helper()
	for _, a := range agentClients {
		if a.ToolkitKey == key {
			return a
		}
	}
	t.Fatalf("no agentClient with ToolkitKey %q", key)
	return agentClient{}
}

// nameFlagValue returns the value passed with claude's -n/--name flag, or "".
func nameFlagValue(args []string) string {
	for i, a := range args {
		if (a == "-n" || a == "--name") && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestClaudeLaunchArgsCarryName(t *testing.T) {
	claude := agentByToolkitKey(t, "claude-code")
	const prompt = "set up RevenueCat"
	for _, autonomy := range []string{autonomyAuto, autonomyTrusted, autonomyFull, autonomyManual} {
		args := claude.LaunchArgs(prompt, autonomy)
		name := nameFlagValue(args)
		if name == "" {
			t.Errorf("autonomy %q: launch args missing -n <name>: %v", autonomy, args)
		}
		if !strings.HasPrefix(name, "RevenueCat setup") {
			t.Errorf("autonomy %q: session name = %q, want prefix %q", autonomy, name, "RevenueCat setup")
		}
		if !containsArg(args, prompt) {
			t.Errorf("autonomy %q: prompt not passed: %v", autonomy, args)
		}
	}
}

func TestNonClaudeLaunchArgsHaveNoName(t *testing.T) {
	for _, key := range []string{"codex", "cursor", "gemini-cli"} {
		agent := agentByToolkitKey(t, key)
		args := agent.LaunchArgs("prompt", autonomyAuto)
		if v := nameFlagValue(args); v != "" {
			t.Errorf("%s: unexpected -n %q in launch args: %v", key, v, args)
		}
	}
}

func TestSetupSessionName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got := setupSessionName()
	if !strings.HasPrefix(got, "RevenueCat setup (") || !strings.HasSuffix(got, ")") {
		t.Fatalf("setupSessionName() = %q, want RevenueCat setup (<dir>)", got)
	}
}
