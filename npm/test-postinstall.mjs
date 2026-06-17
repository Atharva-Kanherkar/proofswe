import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const root = mkdtempSync(join(tmpdir(), "proofswe-postinstall-"));
const codexHome = join(root, "codex");
const claudeHome = join(root, "claude");
const script = resolve("npm/postinstall.js");

const result = spawnSync(process.execPath, [script], {
  encoding: "utf8",
  env: {
    ...process.env,
    CODEX_HOME: codexHome,
    PROOFSWE_CLAUDE_HOME: claudeHome,
    PROOFSWE_QUIET_POSTINSTALL: "1",
  },
});
if (result.status !== 0) {
  throw new Error(`postinstall exited ${result.status}\nstdout=${result.stdout}\nstderr=${result.stderr}`);
}

const codexPrompt = readFileSync(join(codexHome, "prompts", "benchmark.md"), "utf8");
const codexSkill = readFileSync(join(codexHome, "skills", "proofswe-benchmark", "SKILL.md"), "utf8");
const claudeSkill = readFileSync(join(claudeHome, "skills", "benchmark", "SKILL.md"), "utf8");

for (const [name, content] of [
  ["codex prompt", codexPrompt],
  ["codex skill", codexSkill],
  ["claude skill", claudeSkill],
]) {
  if (!content.includes("proofswe submit")) {
    throw new Error(`${name} missing proofswe submit`);
  }
  if (content.includes("OPENAI_API_KEY") || content.includes("ANTHROPIC_API_KEY")) {
    throw new Error(`${name} should not ask for local judge keys`);
  }
}
if (!claudeSkill.includes("name: benchmark")) {
  throw new Error("Claude Code skill should install as /benchmark");
}

console.log("proofswe postinstall smoke passed");
