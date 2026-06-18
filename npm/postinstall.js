#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

function devOverridesEnabled() {
  return process.env.PROOFSWE_ENABLE_DEV_OVERRIDES === "1";
}

function currentPlatform() {
  if (devOverridesEnabled() && process.env.PROOFSWE_TEST_PLATFORM) {
    return process.env.PROOFSWE_TEST_PLATFORM;
  }
  return process.platform;
}

function currentArch() {
  if (devOverridesEnabled() && process.env.PROOFSWE_TEST_ARCH) {
    return process.env.PROOFSWE_TEST_ARCH;
  }
  return process.arch;
}

function packagePlatform(platform) {
  return platform === "win32" ? "windows" : platform;
}

function packageName() {
  const platform = currentPlatform();
  const arch = currentArch();
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
  return `proofswe-${packagePlatform(platform)}-${arch}`;
}

function binaryPath() {
  if (devOverridesEnabled() && process.env.PROOFSWE_BINARY_PATH) {
    return process.env.PROOFSWE_BINARY_PATH;
  }

  const platform = currentPlatform();
  const suffix = platform === "win32" ? ".exe" : "";
  const pkg = packageName();
  if (devOverridesEnabled() && process.env.PROOFSWE_PACKAGE_ROOT) {
    const candidate = path.join(process.env.PROOFSWE_PACKAGE_ROOT, "node_modules", pkg, "bin", `proofswe${suffix}`);
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return require.resolve(`${pkg}/bin/proofswe${suffix}`);
}

if (
  process.env.PROOFSWE_OFF === "1" ||
  process.env.DO_NOT_TRACK === "1" ||
  process.env.PROOFSWE_SKIP_AGENT_INSTALL === "1"
) {
  process.exit(0);
}

try {
  const bin = binaryPath();
  const interactive = Boolean(process.stdin.isTTY && process.stdout.isTTY && process.env.CI !== "true");
  const args = ["agent", "install", "--auto", "--if-missing", "--quiet"];
  if (interactive) {
    args.push("--prompt-code-publication-agreement");
  }
  spawnSync(bin, args, {
    stdio: interactive ? "inherit" : "ignore",
  });
  process.exit(0);
} catch {
  process.exit(0);
}
