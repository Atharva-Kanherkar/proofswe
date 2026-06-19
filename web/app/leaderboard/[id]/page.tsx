import type { Metadata } from "next";
import TranscriptView from "./transcript-view";

export const metadata: Metadata = {
  title: "Session transcript",
  description:
    "A judged coding-agent session from the ProofSWE public corpus — full transcript, outcome, and score breakdown.",
  robots: { index: false, follow: true },
};

export default async function TranscriptPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  return (
    <main className="leaderboard-shell">
      <div className="transcript-page rise">
        <TranscriptView id={id} />
      </div>
    </main>
  );
}
