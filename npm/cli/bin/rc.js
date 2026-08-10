#!/usr/bin/env node
// Launcher for the RevenueCat CLI: resolves the prebuilt Go binary for this
// platform (shipped in a sibling @revenuecat/cli-<platform>-<arch> package)
// and hands over execution. No JavaScript runs beyond this dispatch.
"use strict";
const { execFileSync } = require("child_process");
const path = require("path");

// True when launched via npx (npm's ephemeral `_npx` cache), not a real install.
function runViaNpx() {
  return __dirname.includes(`${path.sep}_npx${path.sep}`) || process.env.npm_command === "exec";
}

function binaryPath() {
  const key = `${process.platform}-${process.arch}`;
  const name = process.platform === "win32" ? "rc.exe" : "rc";
  try {
    return require.resolve(`@revenuecat/cli-${key}/bin/${name}`);
  } catch (err) {
    console.error(`@revenuecat/cli: no prebuilt binary available for ${key}.`);
    console.error(
      "Supported platforms: darwin-arm64, darwin-x64, linux-arm64, linux-x64, win32-arm64, win32-x64."
    );
    console.error(
      "If your platform is listed, your package manager may have skipped optional dependencies (e.g. --no-optional)."
    );
    console.error(
      "Other install options: `brew install RevenueCat/tap/rc` or https://github.com/RevenueCat/revenuecat-cli/releases"
    );
    process.exit(1);
  }
}

// thoth bug-bash build only (never merges): default testers to the unreleased skills branch.
const SKILLS_BRANCH = "rc-cli-astra-paywall-workflows";

const env = { ...process.env };
if (runViaNpx()) env.RC_GUIDED = "1";
if (SKILLS_BRANCH && !env.RC_SKILLS_BRANCH) env.RC_SKILLS_BRANCH = SKILLS_BRANCH;
try {
  execFileSync(binaryPath(), process.argv.slice(2), { stdio: "inherit", env });
} catch (err) {
  if (typeof err.status === "number") process.exit(err.status);
  throw err;
}
