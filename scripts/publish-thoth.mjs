#!/usr/bin/env node
// thoth bug-bash publisher — lives on the throwaway `thoth` branch, never merges.
// Builds the current tip's binaries and publishes the seven @joshdholtz/thoth*
// packages (launcher + 6 platform binaries). The launcher is the repo's
// npm/cli/bin/rc.js with the scope rewritten to @joshdholtz/thoth; it already
// carries the RC_SKILLS_BRANCH default baked in on this branch.
//
//   node scripts/publish-thoth.mjs --version 0.0.2            # pack only (dry run)
//   node scripts/publish-thoth.mjs --version 0.0.2 --publish  # npm publish (needs npm login)
//   [--no-build]  reuse an existing dist/ instead of running goreleaser
"use strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const SCOPE = "@joshdholtz/thoth";
const PLATFORMS = [
  { npm: "darwin-arm64", goos: "darwin", goarch: "arm64", bin: "rc" },
  { npm: "darwin-x64", goos: "darwin", goarch: "amd64", bin: "rc" },
  { npm: "linux-arm64", goos: "linux", goarch: "arm64", bin: "rc" },
  { npm: "linux-x64", goos: "linux", goarch: "amd64", bin: "rc" },
  { npm: "win32-arm64", goos: "windows", goarch: "arm64", bin: "rc.exe" },
  { npm: "win32-x64", goos: "windows", goarch: "amd64", bin: "rc.exe" },
];

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  if (i === -1) return fallback;
  const next = process.argv[i + 1];
  return next && !next.startsWith("--") ? next : true;
}

const version = arg("version");
const publish = Boolean(arg("publish", false));
const distDir = arg("dist", "dist");
const outDir = arg("out", "thoth-dist");
const noBuild = Boolean(arg("no-build", false));
if (!version || version === true) {
  console.error("--version is required (e.g. --version 0.0.2)");
  process.exit(2);
}

if (!noBuild) {
  console.log("building tip-of-stack binaries with goreleaser…");
  // Pin the tag goreleaser derives its version from: the repo's latest git tag
  // isn't semver, which breaks --snapshot's version template.
  execFileSync("goreleaser", ["build", "--snapshot", "--clean"], {
    stdio: "inherit",
    env: { ...process.env, GORELEASER_CURRENT_TAG: `v${version}` },
  });
}

function findBinary(platform) {
  for (const entry of fs.readdirSync(distDir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    if (!entry.name.includes(`_${platform.goos}_${platform.goarch}`)) continue;
    const candidate = path.join(distDir, entry.name, platform.bin);
    if (fs.existsSync(candidate)) return candidate;
  }
  return null;
}

function writeJSON(file, value) {
  fs.writeFileSync(file, JSON.stringify(value, null, 2) + "\n");
}

fs.rmSync(outDir, { recursive: true, force: true });
fs.mkdirSync(outDir, { recursive: true });

const built = [];
for (const platform of PLATFORMS) {
  const binary = findBinary(platform);
  if (!binary) {
    console.error(`error: no ${platform.goos}/${platform.goarch} binary under ${distDir}`);
    process.exit(1);
  }
  const pkgName = `${SCOPE}-${platform.npm}`;
  const pkgDir = path.join(outDir, `thoth-${platform.npm}`);
  fs.mkdirSync(path.join(pkgDir, "bin"), { recursive: true });
  fs.copyFileSync(binary, path.join(pkgDir, "bin", platform.bin));
  fs.chmodSync(path.join(pkgDir, "bin", platform.bin), 0o755);
  writeJSON(path.join(pkgDir, "package.json"), {
    name: pkgName,
    version,
    description: `RevenueCat CLI (bug-bash build) binary for ${platform.npm}`,
    license: "MIT",
    os: [platform.goos === "windows" ? "win32" : platform.goos],
    cpu: [platform.npm.endsWith("arm64") ? "arm64" : "x64"],
    files: [`bin/${platform.bin}`],
  });
  built.push({ pkgName, pkgDir });
}

// Launcher: reuse the repo's rc.js (which carries the skills stamp on this
// branch) and rewrite the package scope from @revenuecat/cli to thoth.
const launcherDir = path.join(outDir, "cli");
fs.mkdirSync(path.join(launcherDir, "bin"), { recursive: true });
const launcherSrc = fs.readFileSync(path.join("npm", "cli", "bin", "rc.js"), "utf8");
const launcher = launcherSrc.replaceAll("@revenuecat/cli", SCOPE);
if (!launcher.includes(`${SCOPE}-`)) {
  console.error("launcher rewrite failed: no thoth-scoped binary reference produced");
  process.exit(1);
}
fs.writeFileSync(path.join(launcherDir, "bin", "rc.js"), launcher);
fs.chmodSync(path.join(launcherDir, "bin", "rc.js"), 0o755);
writeJSON(path.join(launcherDir, "package.json"), {
  name: SCOPE,
  version,
  description: "RevenueCat CLI (bug-bash build)",
  keywords: ["revenuecat", "cli", "paywalls", "subscriptions", "in-app purchases"],
  license: "MIT",
  bin: { rc: "bin/rc.js" },
  files: ["bin/rc.js"],
  engines: { node: ">=18" },
  optionalDependencies: Object.fromEntries(built.map(b => [b.pkgName, version])),
});

// Platform packages publish before the launcher so its optionalDependencies resolve.
for (const dir of [...built.map(b => b.pkgDir), launcherDir]) {
  const args = publish
    ? ["publish", "--access", "public"]
    : ["pack", "--pack-destination", path.resolve(outDir)];
  console.log(`npm ${args[0]}: ${dir}`);
  execFileSync("npm", args, { cwd: dir, stdio: "inherit" });
}
console.log(publish ? `published ${SCOPE}@${version}` : `tarballs in ${outDir}/ (dry run — add --publish)`);
