import WaitlistForm from "./waitlist-form";

// TODO: confirm the real AgentClash URL
const AGENTCLASH_URL = "https://agentclash.com";

export default function Home() {
  return (
    <main className="relative min-h-svh overflow-hidden px-6 py-8 sm:px-12 sm:py-12">
      <div className="glow" aria-hidden="true" />

      {/* top bar — gen-z lineage tag, top right */}
      <header className="relative flex justify-end">
        <a
          href={AGENTCLASH_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="rise font-mono text-[11px] uppercase tracking-[0.25em] text-[var(--muted)] transition-colors hover:text-[var(--fg)]"
          style={{ animationDelay: "0.1s" }}
        >
          an agentclash joint ↗
        </a>
      </header>

      {/* center stack */}
      <section className="relative mx-auto flex min-h-[68svh] max-w-3xl flex-col justify-center">
        <h1
          data-text="ProofSWE"
          className="chrome rise select-none text-[clamp(3.5rem,14vw,9rem)] font-semibold leading-[0.9] tracking-[-0.04em]"
          style={{ animationDelay: "0.2s" }}
        >
          ProofSWE
        </h1>

        <div className="mt-6 sm:mt-8">
          <p
            className="rise text-2xl font-semibold tracking-tight text-[var(--fg)] sm:text-4xl"
            style={{ animationDelay: "0.35s" }}
          >
            benchmarks are dead.
          </p>
          <p
            className="rise text-2xl font-semibold tracking-tight text-[var(--muted)] sm:text-4xl"
            style={{ animationDelay: "0.45s" }}
          >
            proof is not.
          </p>
        </div>

        {/* waitlist */}
        <div
          className="rise mt-12 sm:mt-16"
          style={{ animationDelay: "0.6s" }}
        >
          <p className="mb-4 max-w-md text-base text-[var(--fg)]/80 sm:text-lg">
            free yourself from benchmark pain.
          </p>
          <WaitlistForm />
        </div>
      </section>

      {/* bottom-left status */}
      <footer
        className="rise relative font-mono text-[11px] uppercase tracking-[0.25em] text-[var(--muted)]"
        style={{ animationDelay: "0.75s" }}
      >
        <p>coming soon</p>
        <p className="my-2 h-px w-8 bg-[var(--line)]" aria-hidden="true" />
        <p>proofswe.com</p>
      </footer>
    </main>
  );
}
