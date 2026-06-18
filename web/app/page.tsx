import ModelStrip from "@/components/model-strip";
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
          className="rise select-none text-[2.75rem] font-semibold leading-none text-[var(--fg)] sm:text-[4rem] md:text-[5rem]"
          style={{ animationDelay: "0.2s" }}
        >
          ProofSWE
        </h1>

        <p
          className="rise mt-6 max-w-xl text-2xl font-medium leading-snug text-[var(--fg)] sm:text-3xl md:text-4xl"
          style={{ animationDelay: "0.35s" }}
        >
          benchmarks are dead. proof is not.
        </p>

        <p
          className="rise mt-5 max-w-md text-sm text-[var(--muted)] sm:text-base"
          style={{ animationDelay: "0.45s" }}
        >
          free yourself from benchmark pain.
        </p>

        <a
          href={AGENTCLASH_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="rise mt-10 font-mono text-[12px] text-[var(--muted)] transition-colors hover:text-[var(--fg)]"
          style={{ animationDelay: "0.65s" }}
        >
          an agentclash joint ↗
        </a>
      </div>

      <ModelStrip />
    </main>
  );
}
