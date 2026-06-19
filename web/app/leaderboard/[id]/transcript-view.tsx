"use client";

import Link from "next/link";
import { useEffect, useState } from "react";

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
  task_statement?: string;
  follow_ups?: number;
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

function outcomeChips(outcome: Outcome): { label: string; tone: string }[] {
  const chips: { label: string; tone: string }[] = [];
  if (outcome.verification === "passed")
    chips.push({ label: "tests passed", tone: "good" });
  else if (outcome.verification === "failed")
    chips.push({ label: "tests failed", tone: "bad" });
  else chips.push({ label: "no tests run", tone: "muted" });
  chips.push(
    outcome.landed
      ? { label: "landed (commit/PR)", tone: "good" }
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
    chips.push({ label: "developer approved", tone: "good" });
  if (outcome.interruptions)
    chips.push({ label: `${outcome.interruptions} interrupted`, tone: "bad" });
  if (outcome.files_touched)
    chips.push({
      label: `${outcome.files_touched} file${outcome.files_touched > 1 ? "s" : ""} touched`,
      tone: "muted",
    });
  for (const skill of outcome.skills_used ?? [])
    chips.push({ label: `skill: ${skill}`, tone: "muted" });
  return chips;
}

const ROLE_LABEL: Record<string, string> = {
  developer: "Developer",
  assistant: "Assistant",
  tool_call: "Tool call",
  tool_output: "Tool output",
};

function ConversationTurn({ turn }: { turn: Turn }) {
  const isCode = turn.role === "tool_call" || turn.role === "tool_output";
  return (
    <div className={`turn turn-${turn.role}`}>
      <div className="turn-head">
        <span className="turn-role">{ROLE_LABEL[turn.role] ?? turn.role}</span>
        {turn.name ? <span className="turn-name">{turn.name}</span> : null}
      </div>
      {isCode ? (
        <pre className="turn-code">
          <code>{turn.text}</code>
        </pre>
      ) : (
        <div className="turn-text">{turn.text}</div>
      )}
    </div>
  );
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
  if (status === "notfound") {
    return (
      <div className="transcript-empty">
        <p className="leaderboard-state">This session was not found.</p>
        <Link href="/leaderboard" className="back-link">
          ← Back to leaderboard
        </Link>
      </div>
    );
  }
  if (status === "error" || !data) {
    return (
      <div className="transcript-empty">
        <p className="leaderboard-state">Could not load this session.</p>
        <Link href="/leaderboard" className="back-link">
          ← Back to leaderboard
        </Link>
      </div>
    );
  }

  const u = data.utility ?? {};
  return (
    <article className="transcript">
      <Link href="/leaderboard" className="back-link">
        ← Leaderboard
      </Link>

      <header className="transcript-header">
        <div className="transcript-score" aria-label={`Score ${data.score}`}>
          {data.score}
        </div>
        <div>
          <h1>{data.title || "Session transcript"}</h1>
          <p className="transcript-meta">
            <strong>{data.model || "unknown"}</strong>
            <span>{data.harness}</span>
            {data.repo ? <span>{data.repo}</span> : null}
            <time dateTime={data.published_at}>
              {formatDate(data.published_at)}
            </time>
          </p>
        </div>
      </header>

      {data.outcome ? (
        <div className="chip-row">
          {outcomeChips(data.outcome).map((chip, i) => (
            <span key={i} className={`chip chip-${chip.tone}`}>
              {chip.label}
            </span>
          ))}
        </div>
      ) : null}

      <div className="transcript-grid">
        <section className="transcript-main">
          <h2 className="block-label">Conversation</h2>
          {data.conversation && data.conversation.length > 0 ? (
            <div className="conversation">
              {data.conversation.map((turn, i) => (
                <ConversationTurn key={i} turn={turn} />
              ))}
            </div>
          ) : (
            <p className="leaderboard-state">
              No transcript text was captured for this session.
            </p>
          )}
        </section>

        <aside className="transcript-side">
          {data.axes && data.axes.length > 0 ? (
            <div className="side-block">
              <h2 className="block-label">Score breakdown</h2>
              <div className="axis-list">
                {data.axes.map((axis) => (
                  <div className="axis-row" key={axis.name}>
                    <div className="axis-head">
                      <span className="axis-name">{axis.name}</span>
                      <span className="axis-score">{axis.score}</span>
                    </div>
                    <div className="axis-bar">
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
            </div>
          ) : null}

          {u.evidence && u.evidence.length > 0 ? (
            <div className="side-block">
              <h2 className="block-label">
                Why this score
                {u.confidence ? (
                  <span className="confidence-tag">{u.confidence}</span>
                ) : null}
              </h2>
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
            </div>
          ) : null}

          {data.github_url || data.github_pr_url ? (
            <div className="side-block">
              <h2 className="block-label">Evidence</h2>
              <div className="session-links">
                {data.github_url ? (
                  <a
                    href={data.github_url}
                    target="_blank"
                    rel="noopener noreferrer"
                  >
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
            </div>
          ) : null}
        </aside>
      </div>
    </article>
  );
}
