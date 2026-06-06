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

// The matching platform package is found among this package's own optional
// dependencies, by the -<goos>-<goarch> suffix the packaging step gives them.
// Reading it from the manifest rather than rebuilding the name means the
// launcher can be renamed, or moved under a scope, without the platform
// packages having to follow.
const { optionalDependencies = {} } = require("../package.json");

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

const suffix = `-${goos}-${goarch}`;
const pkg = Object.keys(optionalDependencies).find((d) => d.endsWith(suffix));
const exe = goos === "windows" ? "gowasm.exe" : "gowasm";

if (!pkg) {
  console.error(
    `gowasm: this build declares no platform package for ${goos}/${goarch}.\n` +
      `Install from source instead:\n` +
      `  go install github.com/paulgrammer/gowasm/cmd/gowasm@latest`,
  );
  process.exit(1);
}

let binary;
try {
  // Resolving the manifest rather than the binary keeps this working whatever
  // the platform package declares in "exports".
  binary = path.join(path.dirname(require.resolve(`${pkg}/package.json`)), "bin", exe);
} catch {
  console.error(
    `gowasm: the platform package ${pkg} is not installed.\n` +
      `\n` +
      `It should have arrived automatically as an optional dependency. The\n` +
      `usual cause is a stale metadata cache: if this version was published\n` +
      `very recently, your package manager may have resolved from a cached\n` +
      `copy that did not list it yet, and skipped it silently because\n` +
      `optional dependencies are allowed to fail.\n` +
      `\n` +
      `Reinstalling against the registry fixes it:\n` +
      `  npm install --prefer-online ${pkg}\n` +
      `\n` +
      `If that does not help, install from source instead:\n` +
      `  go install github.com/paulgrammer/gowasm/cmd/gowasm@latest`,
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
