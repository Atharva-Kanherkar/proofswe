"use client";

import { useEffect, useState } from "react";

const API_BASE =
  process.env.NEXT_PUBLIC_PROOFSWE_API_URL ?? "https://api.proofswe.com";

type ModelRow = {
  harness: string;
  model: string;
  submission_count: number;
  average_score: number;
  best_score: number;
  latest_score: number;
  latest_published_at: string;
};

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
  score?: number;
  deterministic?: number;
  judge_nudge?: number;
  confidence?: string;
  logit?: number;
  evidence?: string[];
};

type SubmissionRow = {
  submission_id: string;
  task_id: string;
  harness: string;
  model: string;
  contributor?: string;
  repo?: string;
  score: number;
  summary?: string;
  github_url?: string;
  github_pr_url?: string;
  published_at: string;
  task_statement?: string;
  follow_ups?: number;
  outcome?: Outcome;
  axes?: Axis[];
  utility?: Utility;
  note?: string;
};

type LeaderboardResponse = {
  generated_at: string;
  recent: SubmissionRow[];
  models: ModelRow[];
};

function formatDate(value: string) {
  return new Intl.DateTimeFormat("en", {
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(new Date(value));
}

// outcomeChips turns the deterministic, transcript-derived outcome into a small
// set of labelled chips — the "what happened / what failed" half of the story.
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

function TaskDetail({ session }: { session: SubmissionRow }) {
  const { task_statement, follow_ups, outcome, axes, utility, note } = session;
  return (
    <div className="session-detail">
      {task_statement ? (
        <div className="detail-block">
          <h3>The ask</h3>
          <blockquote className="detail-ask">{task_statement}</blockquote>
          {follow_ups ? (
            <p className="detail-sub">
              + {follow_ups} follow-up developer turn
              {follow_ups > 1 ? "s" : ""}
            </p>
          ) : null}
        </div>
      ) : null}

      {outcome ? (
        <div className="detail-block">
          <h3>What happened</h3>
          <div className="chip-row">
            {outcomeChips(outcome).map((chip, i) => (
              <span key={i} className={`chip chip-${chip.tone}`}>
                {chip.label}
              </span>
            ))}
          </div>
        </div>
      ) : null}

      {axes && axes.length > 0 ? (
        <div className="detail-block">
          <h3>Score breakdown</h3>
          <div className="axis-list">
            {axes.map((axis) => (
              <div className="axis-row" key={axis.name}>
                <div className="axis-head">
                  <span className="axis-name">{axis.name}</span>
                  <span className="axis-score">{axis.score}</span>
                </div>
                <div className="axis-bar">
                  <span style={{ width: `${Math.max(0, Math.min(100, axis.score))}%` }} />
                </div>
                {axis.detail ? (
                  <p className="axis-detail">{axis.detail}</p>
                ) : null}
              </div>
            ))}
          </div>
        </div>
      ) : null}

      {utility &&
      (utility.evidence?.length || utility.logit !== undefined) ? (
        <div className="detail-block">
          <h3>
            Why this score
            {utility.confidence ? (
              <span className="confidence-tag">{utility.confidence} confidence</span>
            ) : null}
          </h3>
          <ul className="evidence-list">
            {(utility.evidence ?? []).map((line, i) => (
              <li key={i}>{line}</li>
            ))}
          </ul>
          <p className="detail-sub">
            {utility.deterministic !== undefined
              ? `deterministic ${utility.deterministic}`
              : null}
            {utility.judge_nudge
              ? ` · judge nudge ${utility.judge_nudge > 0 ? "+" : ""}${utility.judge_nudge}`
              : null}
            {utility.logit !== undefined ? ` · logit ${utility.logit}` : null}
          </p>
        </div>
      ) : null}

      {note ? <p className="detail-note">{note}</p> : null}

      <div className="session-links">
        {session.github_url ? (
          <a href={session.github_url} target="_blank" rel="noopener noreferrer">
            View full transcript ↗
          </a>
        ) : null}
        {session.github_pr_url ? (
          <a href={session.github_pr_url} target="_blank" rel="noopener noreferrer">
            Publication PR ↗
          </a>
        ) : null}
      </div>
    </div>
  );
}

export default function LeaderboardView() {
  const [data, setData] = useState<LeaderboardResponse | null>(null);
  const [failed, setFailed] = useState(false);
  const [open, setOpen] = useState<Set<string>>(new Set());

  useEffect(() => {
    const controller = new AbortController();

    fetch(`${API_BASE.replace(/\/$/, "")}/v1/leaderboard?limit=50`, {
      signal: controller.signal,
    })
      .then((response) => {
        if (!response.ok) {
          throw new Error(`leaderboard request failed: ${response.status}`);
        }
        return response.json() as Promise<LeaderboardResponse>;
      })
      .then(setData)
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === "AbortError") {
          return;
        }
        setFailed(true);
      });

    return () => controller.abort();
  }, []);

  function toggle(id: string) {
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  if (failed) {
    return (
      <p className="leaderboard-state" role="status">
        The public corpus feed is temporarily unavailable.
      </p>
    );
  }

  if (!data) {
    return (
      <p className="leaderboard-state" role="status">
        Loading published sessions…
      </p>
    );
  }

  return (
    <>
      <section className="leaderboard-section" aria-labelledby="model-rankings">
        <div className="leaderboard-section-heading">
          <h2 id="model-rankings">Model rankings</h2>
          <span>{data.models.length} models</span>
        </div>

        {data.models.length === 0 ? (
          <p className="leaderboard-state">No published scores yet.</p>
        ) : (
          <div className="rank-table-wrap">
            <table className="rank-table">
              <thead>
                <tr>
                  <th scope="col">Rank</th>
                  <th scope="col">Model</th>
                  <th scope="col">Harness</th>
                  <th scope="col">Sessions</th>
                  <th scope="col">Average</th>
                  <th scope="col">Best</th>
                </tr>
              </thead>
              <tbody>
                {data.models.map((row, index) => (
                  <tr key={`${row.harness}:${row.model}`}>
                    <td className="rank-number">{index + 1}</td>
                    <th scope="row">{row.model}</th>
                    <td>{row.harness}</td>
                    <td>{row.submission_count}</td>
                    <td className="rank-score">{row.average_score}</td>
                    <td>{row.best_score}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="leaderboard-section" aria-labelledby="recent-sessions">
        <div className="leaderboard-section-heading">
          <h2 id="recent-sessions">Recent sessions</h2>
          <span>Updated {formatDate(data.generated_at)}</span>
        </div>

        <div className="session-list">
          {data.recent.map((session) => {
            const isOpen = open.has(session.submission_id);
            const hasDetail =
              !!session.task_statement ||
              !!session.outcome ||
              (session.axes?.length ?? 0) > 0;
            const panelId = `detail-${session.submission_id}`;
            return (
              <article
                className={`session-row${isOpen ? " is-open" : ""}`}
                key={session.submission_id}
              >
                <button
                  type="button"
                  className="session-summary"
                  aria-expanded={isOpen}
                  aria-controls={panelId}
                  onClick={() => toggle(session.submission_id)}
                  disabled={!hasDetail}
                >
                  <span
                    className="session-score"
                    aria-label={`Score ${session.score}`}
                  >
                    {session.score}
                  </span>
                  <span className="session-main">
                    <span className="session-meta">
                      <strong>{session.model}</strong>
                      <span>{session.harness}</span>
                      {session.repo ? <span>{session.repo}</span> : null}
                      <time dateTime={session.published_at}>
                        {formatDate(session.published_at)}
                      </time>
                    </span>
                    <span className="session-line">
                      {session.task_statement || session.summary ||
                        "Judged session published to the corpus."}
                    </span>
                  </span>
                  {hasDetail ? (
                    <span className="session-chevron" aria-hidden="true">
                      {isOpen ? "▲" : "▼"}
                    </span>
                  ) : null}
                </button>
                {isOpen ? (
                  <div id={panelId}>
                    <TaskDetail session={session} />
                  </div>
                ) : null}
              </article>
            );
          })}
          {data.recent.length === 0 ? (
            <p className="leaderboard-state">No published sessions yet.</p>
          ) : null}
        </div>
      </section>
    </>
  );
}
