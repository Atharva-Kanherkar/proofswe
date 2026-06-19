"use client";

import { useMemo } from "react";
import hljs from "highlight.js";

// Constrain auto-detection to languages that actually show up in agent sessions,
// so highlightAuto doesn't mislabel plain logs as some exotic grammar.
const LANGS = [
  "bash", "shell", "json", "javascript", "typescript", "tsx", "jsx",
  "python", "go", "diff", "yaml", "xml", "html", "css", "scss", "sql",
  "rust", "java", "ruby", "markdown", "ini", "dockerfile", "makefile",
];

function escapeHtml(s: string): string {
  return s.replace(/[&<>]/g, (c) =>
    c === "&" ? "&amp;" : c === "<" ? "&lt;" : "&gt;",
  );
}

function highlight(code: string, lang?: string): string {
  try {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang }).value;
    }
    return hljs.highlightAuto(code, LANGS).value;
  } catch {
    return escapeHtml(code);
  }
}

// HiCode renders a syntax-highlighted block. lang forces a grammar; otherwise the
// language is auto-detected.
export function HiCode({ code, lang }: { code: string; lang?: string }) {
  const html = useMemo(() => highlight(code, lang), [code, lang]);
  return (
    <pre className="code-block">
      <code className="hljs" dangerouslySetInnerHTML={{ __html: html }} />
    </pre>
  );
}

const META_HEAD =
  /^(Chunk ID|Wall time|Process exited|Original token count|Total token count|Tokens?|Exit code|Duration)\b/i;

// parseToolOutput peels the codex execution preamble (Chunk ID / Wall time /
// exit code / token count, then "Output:") off the front, returning compact meta
// chips plus the real body to highlight.
export function parseToolOutput(text: string): { meta: string[]; body: string } {
  let head = "";
  let body = text;
  const firstLine = text.split("\n", 1)[0] ?? "";
  if (META_HEAD.test(firstLine)) {
    const marker = text.match(/\nOutput:\s*\n/);
    if (marker && marker.index !== undefined) {
      head = text.slice(0, marker.index);
      body = text.slice(marker.index + marker[0].length);
    } else {
      const lines = text.split("\n");
      let i = 0;
      while (
        i < lines.length &&
        (META_HEAD.test(lines[i]) || lines[i].trim() === "" || /^Output:/i.test(lines[i]))
      )
        i++;
      head = lines.slice(0, i).join("\n");
      body = lines.slice(i).join("\n");
    }
  }
  const meta: string[] = [];
  const exit = head.match(/exited with code (\d+)/i);
  if (exit) meta.push(exit[1] === "0" ? "exit 0" : `exit ${exit[1]}`);
  const time = head.match(/Wall time:\s*([\d.]+)/i);
  if (time) meta.push(`${time[1]}s`);
  const tok = head.match(/token count:\s*(\d+)/i);
  if (tok) meta.push(`${tok[1]} tok`);
  return { meta, body: body.trim() ? body : text };
}

// formatToolCall surfaces the runnable command from a codex/Claude tool-call
// payload (JSON args) as highlightable code; falls back to pretty JSON.
export function formatToolCall(text: string): { code: string; lang: string } {
  const t = text.trim();
  try {
    const obj = JSON.parse(t);
    if (obj && typeof obj === "object") {
      const cmd = obj.cmd ?? obj.command ?? obj.script;
      if (typeof cmd === "string") return { code: cmd, lang: "bash" };
      return { code: JSON.stringify(obj, null, 2), lang: "json" };
    }
  } catch {
    // not JSON — treat as a raw command
  }
  return { code: t, lang: "" };
}
