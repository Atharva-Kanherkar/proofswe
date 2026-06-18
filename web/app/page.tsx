import WaitlistForm from "./waitlist-form";
import Stars from "@/components/stars";

const AGENTCLASH_URL = "https://agentclash.dev";

export default function Home() {
  return (
    <main className="relative flex min-h-svh flex-col overflow-hidden">
      {/* background layers */}
      <Stars />

      {/* centred editorial hero */}
      <div className="relative z-10 flex flex-1 flex-col items-center justify-center px-6 text-center">
        <h1
          className="rise select-none text-[clamp(2.5rem,8vw,5rem)] font-medium leading-none tracking-[-0.02em] text-[var(--fg)]"
          style={{ animationDelay: "0.2s" }}
        >
          ProofSWE
        </h1>

        <p
          className="rise mt-6 max-w-xl text-[clamp(1.3rem,3.4vw,2.1rem)] leading-snug text-[var(--fg)]"
          style={{ animationDelay: "0.35s" }}
        >
          benchmarks are dead.{" "}
          <span
            style={{
              fontFamily: "var(--font-serif-accent), serif",
              fontStyle: "italic",
              color: "var(--accent)",
            }}
          >
            proof
          </span>{" "}
          is not.
        </p>

        <p
          className="rise mt-5 max-w-md text-sm text-[var(--muted)] sm:text-base"
          style={{ animationDelay: "0.45s" }}
        >
          free yourself from benchmark pain.
        </p>

        <div
          className="rise mt-8 flex w-full max-w-md justify-center"
          style={{ animationDelay: "0.65s" }}
        >
          <WaitlistForm />
        </div>

        <a
          href={AGENTCLASH_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="rise mt-10 font-mono text-[12px] tracking-tight text-[var(--muted)] transition-colors hover:text-[var(--fg)]"
          style={{ animationDelay: "0.8s" }}
        >
          an agentclash joint ↗
        </a>
      </div>
    </main>
  );
}
