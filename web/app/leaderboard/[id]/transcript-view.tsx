"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import Markdown from "./markdown";
import { HiCode, parseToolOutput, formatToolCall } from "./code";

const API_BASE =
  process.env.NEXT_PUBLIC_PROOFSWE_API_URL ?? "https://api.proofswe.com";

type Outcome = {
  verification?: string;
  landed?: boolean;
  termination?: string;
  human_corrections?: number;
  human_acceptances?: number;
  rework_count?: number;
  interruptions?: number;
  files_touched?: number;
  test_files_touched?: number;
  skills_used?: string[];
};

type Axis = { name: string; score: number; detail?: string };
type Utility = {
  deterministic?: number;
  judge_nudge?: number;
  confidence?: string;
  logit?: number;
  evidence?: string[];
};
type Turn = { role: string; name?: string; text: string };

type Detail = {
  submission_id: string;
  model: string;
  harness: string;
  repo?: string;
  title?: string;
  score: number;
  published_at: string;
  outcome?: Outcome;
  axes?: Axis[];
  utility?: Utility;
  note?: string;
  github_url?: string;
  github_pr_url?: string;
  conversation?: Turn[];
};

function formatDate(value: string) {
  return new Intl.DateTimeFormat("en", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(new Date(value));
}

function scoreTone(score: number): string {
  if (score >= 70) return "good";
  if (score >= 45) return "mid";
  return "bad";
}

function outcomeChips(outcome: Outcome): { label: string; tone: string }[] {
  const chips: { label: string; tone: string }[] = [];
  if (outcome.verification === "passed")
    chips.push({ label: "tests passed", tone: "good" });
  else if (outcome.verification === "failed")
    chips.push({ label: "tests failed", tone: "bad" });
  else chips.push({ label: "no tests run", tone: "muted" });
  chips.push(
    outcome.landed
      ? { label: "landed", tone: "good" }
      : { label: "not landed", tone: "muted" },
  );
  if (outcome.termination === "clean")
    chips.push({ label: "clean end", tone: "good" });
  else if (outcome.termination === "abandoned")
    chips.push({ label: "abandoned", tone: "bad" });
  if (outcome.human_corrections)
    chips.push({
      label: `${outcome.human_corrections} correction${outcome.human_corrections > 1 ? "s" : ""}`,
      tone: "bad",
    });
  if (outcome.human_acceptances)
    chips.push({ label: "approved", tone: "good" });
  if (outcome.interruptions)
    chips.push({ label: `${outcome.interruptions} interrupted`, tone: "bad" });
  if (outcome.files_touched)
    chips.push({
      label: `${outcome.files_touched} file${outcome.files_touched > 1 ? "s" : ""}`,
      tone: "muted",
    });
  for (const skill of outcome.skills_used ?? [])
    chips.push({ label: `skill: ${skill}`, tone: "muted" });
  return chips;
}

// isContextBlob flags harness-injected context/instruction turns so they can be
// collapsed by default and stay out of the way of the real conversation.
function isContextBlob(text: string): boolean {
  const s = text.trimStart().toLowerCase();
  return (
    s.startsWith("# agents.md") ||
    s.startsWith("<environment_context") ||
    s.startsWith("<user_instructions") ||
    s.startsWith("<system-reminder") ||
    s.startsWith("<local-command") ||
    s.startsWith("<command-") ||
    (s.includes("## skills") && s.includes("skill.md"))
  );
}

function lineCount(text: string): number {
  return text ? text.split("\n").length : 0;
}

function ToolCall({ turn }: { turn: Turn }) {
  const { code, lang } = formatToolCall(turn.text);
  return (
    <details className="turn turn-tool" open>
      <summary>
        <span className="turn-icon">⚙</span>
        <span className="turn-role">Tool call</span>
        {turn.name ? <span className="turn-name">{turn.name}</span> : null}
        {lang ? <span className="lang-tag">{lang}</span> : null}
      </summary>
      <HiCode code={code} lang={lang || undefined} />
    </details>
  );
}

function ToolOutput({ turn }: { turn: Turn }) {
  const { meta, body } = parseToolOutput(turn.text);
  return (
    <details className="turn turn-output">
      <summary>
        <span className="turn-icon">↳</span>
        <span className="turn-role">Output</span>
        {turn.name ? <span className="turn-name">{turn.name}</span> : null}
        {meta.map((m, i) => (
          <span key={i} className="meta-chip">
            {m}
          </span>
        ))}
        <span className="turn-hint">{lineCount(body)} lines</span>
      </summary>
      <HiCode code={body} />
    </details>
  );
}

function ProseTurn({ turn }: { turn: Turn }) {
  const isDev = turn.role === "developer";
  const context = isDev && isContextBlob(turn.text);
  const label = isDev ? "Developer" : "Assistant";
  const body = <Markdown>{turn.text}</Markdown>;

  if (context) {
    return (
      <details className="turn turn-context">
        <summary>
          <span className="turn-role">{label}</span>
          <span className="turn-hint">session context / instructions</span>
        </summary>
        {body}
      </details>
    );
  }

  return (
    <div className={`turn turn-prose turn-${turn.role}`}>
      <div className="turn-head">
        <span className="turn-avatar" aria-hidden="true">
          {isDev ? "you" : "ai"}
        </span>
        <span className="turn-role">{label}</span>
      </div>
      {body}
    </div>
  );
}

function ConversationTurn({ turn }: { turn: Turn }) {
  if (turn.role === "tool_call") return <ToolCall turn={turn} />;
  if (turn.role === "tool_output") return <ToolOutput turn={turn} />;
  return <ProseTurn turn={turn} />;
}

export default function TranscriptView({ id }: { id: string }) {
  const [data, setData] = useState<Detail | null>(null);
  const [status, setStatus] = useState<"loading" | "ok" | "notfound" | "error">(
    "loading",
  );

  useEffect(() => {
    const controller = new AbortController();
    fetch(`${API_BASE.replace(/\/$/, "")}/v1/leaderboard/${id}`, {
      signal: controller.signal,
    })
      .then((res) => {
        if (res.status === 404) {
          setStatus("notfound");
          return null;
        }
        if (!res.ok) throw new Error(`status ${res.status}`);
        return res.json() as Promise<Detail>;
      })
      .then((d) => {
        if (d) {
          setData(d);
          setStatus("ok");
        }
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") return;
        setStatus("error");
      });
    return () => controller.abort();
  }, [id]);

  if (status === "loading") {
    return (
      <p className="leaderboard-state" role="status">
        Loading session…
      </p>
    );
  }
  if (status === "notfound" || status === "error" || !data) {
    return (
      <div className="transcript-empty">
        <Link href="/leaderboard" className="back-link">
          ← Leaderboard
        </Link>
        <p className="leaderboard-state">
          {status === "notfound"
            ? "This session was not found."
            : "Could not load this session."}
        </p>
      </div>
    );
  }

  const u = data.utility ?? {};
  const turns = data.conversation ?? [];

  return (
    <article className="transcript">
      <Link href="/leaderboard" className="back-link">
        ← Leaderboard
      </Link>

      <header className="transcript-header">
        <div
          className={`transcript-score score-${scoreTone(data.score)}`}
          aria-label={`Score ${data.score}`}
        >
          {data.score}
          <small>/ 100</small>
        </div>
        <div className="transcript-headmain">
          <h1>{data.title || "Session transcript"}</h1>
          <p className="transcript-meta">
            <strong>{data.model || "unknown"}</strong>
            <span>{data.harness}</span>
            {data.repo ? <span>{data.repo}</span> : null}
            <time dateTime={data.published_at}>
              {formatDate(data.published_at)}
            </time>
          </p>
          {data.outcome ? (
            <div className="chip-row">
              {outcomeChips(data.outcome).map((chip, i) => (
                <span key={i} className={`chip chip-${chip.tone}`}>
                  {chip.label}
                </span>
              ))}
            </div>
          ) : null}
        </div>
      </header>

      {data.axes && data.axes.length > 0 ? (
        <section className="scorecard">
          <div className="axis-grid">
            {data.axes.map((axis) => (
              <div className="axis-cell" key={axis.name}>
                <div className="axis-head">
                  <span className="axis-name">{axis.name}</span>
                  <span className="axis-score">{axis.score}</span>
                </div>
                <div className={`axis-bar axis-${scoreTone(axis.score)}`}>
                  <span
                    style={{
                      width: `${Math.max(0, Math.min(100, axis.score))}%`,
                    }}
                  />
                </div>
                {axis.detail ? (
                  <p className="axis-detail">{axis.detail}</p>
                ) : null}
              </div>
            ))}
          </div>
          {u.evidence && u.evidence.length > 0 ? (
            <details className="why">
              <summary>
                Why this score
                {u.confidence ? (
                  <span className="confidence-tag">{u.confidence}</span>
                ) : null}
              </summary>
              <ul className="evidence-list">
                {u.evidence.map((line, i) => (
                  <li key={i}>{line}</li>
                ))}
              </ul>
              <p className="detail-sub">
                {u.deterministic !== undefined
                  ? `deterministic ${u.deterministic}`
                  : null}
                {u.judge_nudge
                  ? ` · judge ${u.judge_nudge > 0 ? "+" : ""}${u.judge_nudge}`
                  : null}
              </p>
            </details>
          ) : null}
          {data.github_url || data.github_pr_url ? (
            <div className="session-links">
              {data.github_url ? (
                <a href={data.github_url} target="_blank" rel="noopener noreferrer">
                  Corpus task JSON ↗
                </a>
              ) : null}
              {data.github_pr_url ? (
                <a
                  href={data.github_pr_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Publication PR ↗
                </a>
              ) : null}
            </div>
          ) : null}
        </section>
      ) : null}

      <section className="conversation-wrap">
        <div className="conversation-heading">
          <h2>Conversation</h2>
          <span>{turns.length} turns</span>
        </div>
        {turns.length > 0 ? (
          <div className="conversation">
            {turns.map((turn, i) => (
              <ConversationTurn key={i} turn={turn} />
            ))}
          </div>
        ) : (
          <p className="leaderboard-state">
            No transcript text was captured for this session.
          </p>
        )}
      </section>
    </article>
  );
}
