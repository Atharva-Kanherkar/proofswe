"use client";

import Link from "next/link";
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

type SubmissionRow = {
  submission_id: string;
  harness: string;
  model: string;
  repo?: string;
  title?: string;
  score: number;
  summary?: string;
  published_at: string;
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

export default function LeaderboardView() {
  const [data, setData] = useState<LeaderboardResponse | null>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    fetch(`${API_BASE.replace(/\/$/, "")}/v1/leaderboard?limit=100`, {
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
        if (error instanceof DOMException && error.name === "AbortError") return;
        setFailed(true);
      });
    return () => controller.abort();
  }, []);

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
                    <th scope="row">{row.model || "unknown"}</th>
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
          <span>
            {data.recent.length} session{data.recent.length === 1 ? "" : "s"} ·
            updated {formatDate(data.generated_at)}
          </span>
        </div>

        <div className="session-list">
          {data.recent.map((session) => (
            <Link
              className="session-row"
              key={session.submission_id}
              href={`/leaderboard/${session.submission_id}`}
            >
              <span
                className="session-score"
                aria-label={`Score ${session.score}`}
              >
                {session.score}
              </span>
              <span className="session-main">
                <span className="session-title">
                  {session.title || "Untitled session"}
                </span>
                <span className="session-meta">
                  <strong>{session.model || "unknown"}</strong>
                  <span>{session.harness}</span>
                  {session.repo ? <span>{session.repo}</span> : null}
                  <time dateTime={session.published_at}>
                    {formatDate(session.published_at)}
                  </time>
                </span>
                {session.summary ? (
                  <span className="session-line">{session.summary}</span>
                ) : null}
              </span>
              <span className="session-chevron" aria-hidden="true">
                →
              </span>
            </Link>
          ))}
          {data.recent.length === 0 ? (
            <p className="leaderboard-state">No published sessions yet.</p>
          ) : null}
        </div>
      </section>
    </>
  );
}
