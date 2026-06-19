import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Blog",
  description: "ProofSWE notes and essays.",
  alternates: { canonical: "/blog" },
};

export default function BlogPage() {
  return (
    <main className="flex min-h-svh flex-col items-center justify-center px-6 py-20 text-center">
      <div className="rise max-w-2xl" style={{ animationDelay: "0.05s" }}>
        <Link href="/" className="hero-link inline-flex">
          ProofSWE
        </Link>

        <h1 className="hero-tagline mt-8">Blog</h1>

        <p className="hero-sub mt-4">
          Notes on coding agents, software engineering evaluation, and proof.
        </p>

        <Link
          href="/blog/benchmarks-are-dead"
          className="blog-card mt-10 text-left"
        >
          <span className="blog-card-kicker">Essay</span>
          <span className="blog-card-title">Benchmarks Are Dead</span>
          <span className="blog-card-desc">
            Every lab tops the benchmark. Real software work still feels hard.
            The next benchmark has to be the session.
          </span>
        </Link>
      </div>
    </main>
  );
}
