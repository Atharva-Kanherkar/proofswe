import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Blog",
  description: "ProofSWE notes and essays.",
  alternates: { canonical: "/blog" },
};

export default function BlogPage() {
  return (
    <main className="blog-index-shell">
      <div className="blog-index rise" style={{ animationDelay: "0.05s" }}>
        <Link href="/" className="blog-kicker">
          ProofSWE
        </Link>

        <header className="blog-index-header">
          <h1>Blog</h1>
          <p>
            Notes on coding agents, software engineering evaluation, and proof.
          </p>
        </header>

        <div className="blog-list" aria-label="All blog posts">
          <Link href="/blog/benchmarks-are-dead" className="blog-card">
            <span className="blog-card-meta">Essay · June 19, 2026</span>
            <span className="blog-card-title">
              The Next Unit of AI Evaluation Is the Session, not Benchmarks.
            </span>
            <span className="blog-card-desc">
              Every lab tops the benchmark. Real software work still feels hard.
              The next benchmark has to be the session.
            </span>
          </Link>
        </div>
      </div>
    </main>
  );
}
