import { mkdtempSync, mkdirSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const root = mkdtempSync(join(tmpdir(), "proofswe-wrapper-"));
const wrapper = resolve("npm/bin/proofswe.js");
const platforms = [
  ["darwin", "arm64"],
  ["darwin", "x64"],
  ["linux", "arm64"],
  ["linux", "x64"],
  ["win32", "arm64"],
  ["win32", "x64"],
];

for (const [platform, arch] of platforms) {
  const suffix = platform === "win32" ? ".exe" : "";
  const binDir = join(root, "node_modules", `proofswe-${platform}-${arch}`, "bin");
  mkdirSync(binDir, { recursive: true });
  const bin = join(binDir, `proofswe${suffix}`);
  writeFileSync(bin, "#!/bin/sh\necho 'proofswe test-native'\n");
  chmodSync(bin, 0o755);

  const result = spawnSync(process.execPath, [wrapper, "version"], {
    encoding: "utf8",
    env: {
      ...process.env,
      PROOFSWE_ENABLE_DEV_OVERRIDES: "1",
      PROOFSWE_PACKAGE_ROOT: root,
      PROOFSWE_TEST_PLATFORM: platform,
      PROOFSWE_TEST_ARCH: arch,
    },
  });
  if (result.status !== 0) {
    throw new Error(`${platform}/${arch} exited ${result.status}\nstdout=${result.stdout}\nstderr=${result.stderr}`);
  }
  if (!result.stdout.includes("proofswe test-native")) {
    throw new Error(`${platform}/${arch} did not execute native binary\nstdout=${result.stdout}\nstderr=${result.stderr}`);
  }
}

console.log("proofswe npm wrapper smoke passed");
