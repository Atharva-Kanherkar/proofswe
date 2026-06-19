const AGENTCLASH_URL = "https://agentclash.dev";

export default function Home() {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center px-6 py-20 text-center">
      <h1
        className="rise hero-title select-none"
        style={{ animationDelay: "0.05s" }}
      >
        ProofSWE
      </h1>

      <p
        className="rise hero-tagline mt-6 max-w-2xl"
        style={{ animationDelay: "0.15s" }}
      >
        benchmarks are dead. proof is not.
      </p>

      <p
        className="rise hero-sub mt-4 max-w-md"
        style={{ animationDelay: "0.25s" }}
      >
        free yourself from benchmark pain, launching soon.
      </p>

      <a
        href={AGENTCLASH_URL}
        target="_blank"
        rel="noopener noreferrer"
        className="rise hero-link mt-10 inline-flex items-center gap-1.5 transition-colors"
        style={{ animationDelay: "0.4s" }}
      >
        an agentclash joint
        <span aria-hidden="true">↗</span>
      </a>
    </main>
  );
}
