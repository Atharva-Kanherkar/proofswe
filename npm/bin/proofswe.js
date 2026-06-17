#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

function packageName() {
  const platform = process.platform;
  const arch = process.arch;
  const supported = new Set([
    "darwin:arm64",
    "darwin:x64",
    "linux:arm64",
    "linux:x64",
    "win32:arm64",
    "win32:x64",
  ]);
  const key = `${platform}:${arch}`;
  if (!supported.has(key)) {
    throw new Error(`unsupported platform ${platform}/${arch}`);
  }
  return `@proofswe/${platform}-${arch}`;
}

function binaryPath() {
  if (process.env.PROOFSWE_BINARY_PATH) {
    return process.env.PROOFSWE_BINARY_PATH;
  }

  const suffix = process.platform === "win32" ? ".exe" : "";
  const pkg = packageName();
  try {
    return require.resolve(`${pkg}/bin/proofswe${suffix}`);
  } catch (err) {
    const local = path.resolve(__dirname, "..", "..", "dist", `proofswe${suffix}`);
    if (fs.existsSync(local)) {
      return local;
    }
    throw new Error(
      `could not find native proofswe binary package ${pkg}; reinstall with optional dependencies enabled`
    );
  }
}

let bin;
try {
  bin = binaryPath();
} catch (err) {
  console.error(`proofswe: ${err.message}`);
  process.exit(1);
}

const result = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (result.error) {
  console.error(`proofswe: ${result.error.message}`);
  process.exit(1);
}
process.exit(result.status === null ? 1 : result.status);
