#!/usr/bin/env node
"use strict";
// Launcher for the gowasm binary.
//
// The binaries live in one small package per platform, listed as optional
// dependencies of this one. npm resolves the `os` and `cpu` fields and installs
// only the matching package, so nothing is downloaded at install time by a
// script: there is no postinstall step, which means this works with
// --ignore-scripts, offline caches and locked-down CI.
const { spawnSync } = require("node:child_process");
const path = require("node:path");

// The platform packages are named after this one, so the prefix is read from
// the manifest rather than written twice. Renaming the package renames them
// with it, and the two cannot drift apart.
const { name: PKG_NAME } = require("../package.json");

const PLATFORMS = { darwin: "darwin", linux: "linux", win32: "windows" };
const ARCHS = { x64: "amd64", arm64: "arm64" };

const goos = PLATFORMS[process.platform];
const goarch = ARCHS[process.arch];

if (!goos || !goarch) {
  console.error(
    `gowasm: no prebuilt binary for ${process.platform}/${process.arch}.\n` +
      `Install from source instead:\n` +
      `  go install github.com/paulgrammer/gowasm/cmd/gowasm@latest`,
  );
  process.exit(1);
}

const pkg = `${PKG_NAME}-${goos}-${goarch}`;
const exe = goos === "windows" ? "gowasm.exe" : "gowasm";

let binary;
try {
  // Resolving the manifest rather than the binary keeps this working whatever
  // the platform package declares in "exports".
  binary = path.join(path.dirname(require.resolve(`${pkg}/package.json`)), "bin", exe);
} catch {
  console.error(
    `gowasm: the platform package ${pkg} is not installed.\n` +
      `It should have come in automatically as an optional dependency.\n` +
      `Try reinstalling, or install it directly:\n` +
      `  npm install ${pkg}`,
  );
  process.exit(1);
}

const result = spawnSync(binary, process.argv.slice(2), { stdio: "inherit" });

if (result.error) {
  console.error(`gowasm: could not run ${binary}: ${result.error.message}`);
  process.exit(1);
}
// Re-raise a fatal signal so callers see the real cause rather than a bare
// non-zero exit.
if (result.signal) {
  process.kill(process.pid, result.signal);
}
process.exit(result.status ?? 1);
