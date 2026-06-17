#!/usr/bin/env node

const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");

const codexBenchmarkPrompt = `Run the proofswe benchmark submission for the current coding-agent session.

Steps:
1. From the repo root, run \`proofswe submit\` and wait for the official scorecard.
2. If \`proofswe\` is not on PATH, run \`npx -y proofswe submit\`.
3. Do not ask the user for an OpenAI or Anthropic key; server submission uses the proofswe hosted judge.
4. If submission fails because the task is not reproducible, report the exact reason and do not force-submit unless the user explicitly asks.
5. Return the score, status, submission URL, and corpus PR/path if present.
`;

const codexSkill = `---
name: proofswe-benchmark
description: Run proofswe benchmarking/submission from inside a coding-agent chat. Use when the user asks to benchmark the current agent session, donate the transcript, submit to proofswe, get an official scorecard, run /benchmark, or avoid leaving the chat to run the CLI.
---

# Proofswe Benchmark

Run the benchmark from the current repository without asking the user to leave the agent chat.

1. Prefer \`proofswe submit\` from the repo root. It auto-detects the latest Claude Code or Codex transcript and waits for the official hosted scorecard.
2. If \`proofswe\` is unavailable, use \`npx -y proofswe submit\`.
3. Use \`proofswe submit --json\` when structured output is easier to summarize.
4. Use \`--no-wait\` only when the user wants to queue the submission and continue immediately.
5. Never ask for a local judge API key for \`submit\`; hosted submission does the official judging. Local keys are only for \`proofswe score --local-judge\` previews.
6. If reproducibility checks fail, show the exact blocker. Do not use \`--force\` unless the user explicitly asks.
7. Report the official score, status, submission URL, and corpus PR/path when present.
`;

const claudeSkill = `---
name: benchmark
description: Run proofswe benchmarking/submission from inside Claude Code. Use when the user asks to benchmark the current Claude Code session, donate the transcript, submit to proofswe, get an official scorecard, run /benchmark, or avoid leaving Claude Code to run the CLI.
---

# Proofswe Benchmark

Run the benchmark from the current repository without asking the user to leave Claude Code.

1. Run \`proofswe submit --harness=claudecode\` from the repo root. It auto-detects the latest Claude Code transcript and waits for the official hosted scorecard.
2. If \`proofswe\` is unavailable, use \`npx -y proofswe submit --harness=claudecode\`.
3. Use \`proofswe submit --harness=claudecode --json\` when structured output is easier to summarize.
4. Use \`--no-wait\` only when the user wants to queue the submission and continue immediately.
5. Never ask for a local judge API key for \`submit\`; hosted submission does the official judging. Local keys are only for \`proofswe score --local-judge\` previews.
6. If reproducibility checks fail, show the exact blocker. Do not use \`--force\` unless the user explicitly asks.
7. Report the official score, status, submission URL, and corpus PR/path when present.
`;

function homeDir() {
  return process.env.HOME || process.env.USERPROFILE || os.homedir();
}

function writeFile(file, content) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, content, { mode: 0o644 });
}

function install() {
  if (process.env.PROOFSWE_SKIP_AGENT_INSTALL === "1") {
    return [];
  }
  const home = homeDir();
  if (!home) {
    return [];
  }
  const codexHome = process.env.CODEX_HOME || path.join(home, ".codex");
  const claudeHome = process.env.PROOFSWE_CLAUDE_HOME || path.join(home, ".claude");
  const installed = [
    [path.join(codexHome, "prompts", "benchmark.md"), codexBenchmarkPrompt],
    [path.join(codexHome, "skills", "proofswe-benchmark", "SKILL.md"), codexSkill],
    [path.join(claudeHome, "skills", "benchmark", "SKILL.md"), claudeSkill],
  ];
  for (const [file, content] of installed) {
    writeFile(file, content);
  }
  return installed.map(([file]) => file);
}

try {
  const installed = install();
  if (installed.length > 0 && process.env.PROOFSWE_QUIET_POSTINSTALL !== "1") {
    console.log("proofswe: installed agent benchmark skills");
    for (const file of installed) {
      console.log(`proofswe: ${file}`);
    }
  }
} catch (err) {
  console.warn(`proofswe: skipped agent skill install: ${err.message}`);
}

