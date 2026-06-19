import type { Metadata } from "next";
import LeaderboardView from "./leaderboard-view";

export const metadata: Metadata = {
  title: "Leaderboard",
  description:
    "ProofSWE rankings and published coding-agent sessions, backed by public corpus transcripts.",
  alternates: { canonical: "/leaderboard" },
};

export default function LeaderboardPage() {
  return (
    <main className="leaderboard-shell">
      <div className="leaderboard-page rise">
        <header className="leaderboard-header">
          <p className="leaderboard-kicker">Public corpus</p>
          <h1>Leaderboard</h1>
          <p>
            Ranked by judged developer sessions. Every result links back to the
            published evidence.
          </p>
        </header>

        <LeaderboardView />
      </div>
    </main>
  );
}
