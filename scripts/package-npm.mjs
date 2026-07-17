#!/usr/bin/env node
// Packages the GoReleaser build output as npm packages: one platform package
// per prebuilt binary (@revenuecat/cli-<platform>-<arch>) plus the
// @revenuecat/cli launcher whose optionalDependencies pin the same version.
//
//   node scripts/package-npm.mjs --version 0.2.0 --dist dist --out npm-dist
//     [--publish]        npm publish (requires NODE_AUTH_TOKEN) instead of npm pack
//     [--allow-missing]  tolerate absent platforms (local dry runs on one machine)
//
// Run from the repo root after `goreleaser release` (or `goreleaser build`).
"use strict";
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

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
const distDir = arg("dist", "dist");
const outDir = arg("out", "npm-dist");
const publish = Boolean(arg("publish", false));
const allowMissing = Boolean(arg("allow-missing", false));
if (!version || version === true) {
  console.error("--version is required (e.g. --version 0.2.0)");
  process.exit(2);
}

// GoReleaser lays binaries out as dist/<build-id>_<goos>_<goarch>[_<goamd64>]/<binary>.
// (Documented layout: https://goreleaser.com/customization/builds/#build-output —
// if a GoReleaser upgrade changes this, findBinary is the only place to touch.)
function findBinary(platform) {
  const entries = fs.readdirSync(distDir, { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    const marker = `_${platform.goos}_${platform.goarch}`;
    if (!entry.name.includes(marker)) continue;
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
    const message = `no ${platform.goos}/${platform.goarch} binary under ${distDir}`;
    if (allowMissing) {
      console.warn(`skip: ${message}`);
      continue;
    }
    console.error(`error: ${message}`);
    process.exit(1);
  }
  const pkgName = `@revenuecat/cli-${platform.npm}`;
  const pkgDir = path.join(outDir, `cli-${platform.npm}`);
  fs.mkdirSync(path.join(pkgDir, "bin"), { recursive: true });
  fs.copyFileSync(binary, path.join(pkgDir, "bin", platform.bin));
  fs.chmodSync(path.join(pkgDir, "bin", platform.bin), 0o755);
  writeJSON(path.join(pkgDir, "package.json"), {
    name: pkgName,
    version,
    description: `RevenueCat CLI binary for ${platform.npm}`,
    homepage: "https://github.com/RevenueCat/revenuecat-cli",
    repository: { type: "git", url: "git+https://github.com/RevenueCat/revenuecat-cli.git" },
    license: "MIT",
    os: [platform.goos === "windows" ? "win32" : platform.goos],
    cpu: [platform.npm.endsWith("arm64") ? "arm64" : "x64"],
    files: [`bin/${platform.bin}`],
  });
  built.push({ pkgName, pkgDir });
}

// Launcher: copy the checked-in package and stamp version + exact-version
// optionalDependencies for the platforms that were actually built.
const launcherSrc = path.join("npm", "cli");
const launcherDir = path.join(outDir, "cli");
fs.mkdirSync(path.join(launcherDir, "bin"), { recursive: true });
fs.copyFileSync(path.join(launcherSrc, "bin", "rc.js"), path.join(launcherDir, "bin", "rc.js"));
fs.chmodSync(path.join(launcherDir, "bin", "rc.js"), 0o755);
fs.copyFileSync(path.join(launcherSrc, "README.md"), path.join(launcherDir, "README.md"));
const launcherPkg = JSON.parse(fs.readFileSync(path.join(launcherSrc, "package.json"), "utf8"));
launcherPkg.version = version;
launcherPkg.optionalDependencies = Object.fromEntries(built.map(b => [b.pkgName, version]));
writeJSON(path.join(launcherDir, "package.json"), launcherPkg);

// Platform packages publish before the launcher so the launcher's
// optionalDependencies always resolve.
const publishOrder = [...built.map(b => b.pkgDir), launcherDir];
for (const dir of publishOrder) {
  const args = publish
    ? ["publish", "--access", "public"]
    : ["pack", "--pack-destination", path.resolve(outDir)];
  console.log(`npm ${args[0]}: ${dir}`);
  execFileSync("npm", args, { cwd: dir, stdio: "inherit" });
}
console.log(publish ? "published" : `tarballs in ${outDir}/`);
