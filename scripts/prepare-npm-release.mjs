import { copyFileSync, chmodSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const version = (process.env.PROOFSWE_RELEASE_VERSION || "").replace(/^v/, "");
if (!version) {
  throw new Error("PROOFSWE_RELEASE_VERSION is required");
}

const platforms = [
  ["darwin", "arm64"],
  ["darwin", "x64"],
  ["linux", "arm64"],
  ["linux", "x64"],
  ["win32", "arm64"],
  ["win32", "x64"],
];

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function writeJSON(path, value) {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

const rootPackage = readJSON("package.json");
rootPackage.version = version;
for (const [platform, arch] of platforms) {
  rootPackage.optionalDependencies[`@proofswe/${platform}-${arch}`] = version;
}
writeJSON("package.json", rootPackage);

for (const [platform, arch] of platforms) {
  const suffix = platform === "win32" ? ".exe" : "";
  const packageDir = join("npm", "native", `${platform}-${arch}`);
  const packageJSONPath = join(packageDir, "package.json");
  const packageJSON = readJSON(packageJSONPath);
  packageJSON.version = version;
  writeJSON(packageJSONPath, packageJSON);

  const binDir = join(packageDir, "bin");
  mkdirSync(binDir, { recursive: true });
  const target = join(binDir, `proofswe${suffix}`);
  copyFileSync(join("dist", `proofswe-${platform}-${arch}${suffix}`), target);
  chmodSync(target, 0o755);
}
