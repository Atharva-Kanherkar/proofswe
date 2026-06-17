import { copyFileSync, chmodSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
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

function walkFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      out.push(...walkFiles(path));
    } else if (stat.isFile()) {
      out.push(path);
    }
  }
  return out;
}

function goReleaserPlatform(platform) {
  return platform === "win32" ? "windows" : platform;
}

function archAliases(arch) {
  if (arch === "x64") {
    return ["amd64", "x86_64", "x64"];
  }
  return [arch];
}

function findNativeBinary(platform, arch, suffix) {
  const files = walkFiles("dist");
  const wantedName = `proofswe${suffix}`;
  const platformAlias = goReleaserPlatform(platform);
  const archNames = archAliases(arch);
  const exact = join("dist", `proofswe-${platform}-${arch}${suffix}`);

  const candidates = files.filter((file) => {
    const lower = file.toLowerCase();
    const base = lower.split(/[\\/]/).pop();
    return base === wantedName && lower.includes(platformAlias) && archNames.some((name) => lower.includes(name));
  });
  if (candidates.length > 0) {
    candidates.sort();
    return candidates[0];
  }
  if (files.includes(exact)) {
    return exact;
  }
  throw new Error(`could not find GoReleaser binary for ${platform}/${arch} under dist/`);
}

const rootPackage = readJSON("package.json");
rootPackage.version = version;
for (const [platform, arch] of platforms) {
  rootPackage.optionalDependencies[`proofswe-${platform}-${arch}`] = version;
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
  copyFileSync(findNativeBinary(platform, arch, suffix), target);
  chmodSync(target, 0o755);
}
