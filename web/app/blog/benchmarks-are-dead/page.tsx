import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Benchmarks Are Dead",
  description:
    "Why coding benchmark leaderboards are useful signals, but weak proof that software engineering is solved.",
  alternates: { canonical: "/blog/benchmarks-are-dead" },
  openGraph: {
    title: "Benchmarks Are Dead",
    description:
      "Coding benchmarks move fast. Proof needs real sessions, reproducible tasks, execution, and review.",
    type: "article",
  },
};

export default function BenchmarksAreDeadPage() {
  return (
    <main className="blog-shell">
      <article className="blog-article">
        <header className="blog-header rise" style={{ animationDelay: "0.05s" }}>
          <Link href="/blog" className="blog-kicker">
            Blog
          </Link>
          <h1>Benchmarks Are Dead</h1>
          <p className="blog-dek">
            Leaderboards still matter. They are useful instruments. They are
            just no longer enough to prove that an agent can do software
            engineering.
          </p>
          <p className="blog-meta">June 19, 2026</p>
        </header>

        <section className="blog-section rise" style={{ animationDelay: "0.1s" }}>
          <p>
            The old benchmark story was simple: publish a score, top the table,
            declare progress. That story has collapsed under its own success.
            The serious coding benchmarks now move so quickly that a leaderboard
            can say more about timing, harness design, and test selection than
            about whether a model can carry a messy engineering task to done.
          </p>
          <p>
            This does not mean benchmarks are useless. It means they have
            stopped being proof. Software engineering is not solved when a model
            passes a tidy public test. It is closer to solved when work from a
            real session can be replayed, audited, executed, reviewed, and
            compared against the actual developer intent.
          </p>
        </section>

        <section className="blog-section">
          <h2>The benchmark was supposed to be hard. Then everyone trained for it.</h2>
          <p>
            SWE-bench was important because it moved evaluation from toy code
            completion toward real GitHub issues. The original paper introduced
            2,294 software engineering problems from 12 popular Python
            repositories, and reported that Claude 2 solved only 1.96 percent of
            issues in the initial evaluation.
          </p>
          <p>
            That was the right kind of shock. It showed that generating snippets
            and fixing real repositories are different activities. Real issues
            require reading a codebase, forming a patch, running tests, and
            coordinating changes across files. A public number near zero was a
            useful correction to model-release optimism.
          </p>
          <p>
            But once a benchmark becomes the headline, the market optimizes for
            the headline. OpenAI later introduced SWE-bench Verified as a
            human-validated 500-task subset because annotators needed to check
            whether problem descriptions were clear, test patches were correct,
            and tasks were solvable from the available information. That
            improvement was valuable, but it also proved the point: even the
            benchmark had to become a better engineered product.
          </p>
        </section>

        <section className="blog-section">
          <h2>The churn is not noise. The churn is the signal.</h2>
          <p>
            In April 2025, OpenAI reported GPT-4.1 at 54.6 percent on SWE-bench
            Verified, a large jump over GPT-4o and GPT-4.5. Earlier that
            year, Anthropic reported Claude 3.7 Sonnet at 70.3 percent on 489
            verified tasks with a custom scaffold, while the same model scored
            63.7 percent without that scaffold.
            Google reported Gemini 2.5 Pro at 63.8 percent on SWE-bench Verified
            with a custom agent setup.
            Anthropic later reported Claude Opus 4 leading on SWE-bench at 72.5
            percent.
          </p>
          <p>
            Each number may be honest. The problem is that they are not the same
            object. A model plus a scaffold is not the same as a model alone. A
            custom harness is not the same as an open harness. A 489-task subset
            is not the same as a 500-task subset. A leaderboard snapshot is not
            the same as a reproducible claim.
          </p>
          <p>
            LMArena makes the churn visible. Its Text Coding leaderboard listed
            1,366,264 votes across 362 models on June 16, 2026, with
            claude-fable-5 at the top. Its
            WebDev leaderboard listed 381,168 votes across 89 models on the same
            date, again with claude-fable-5 at the top. The changelog
            shows a steady stream of models being added to text, code, vision,
            search, document, image, and video leaderboards through early 2026.
          </p>
          <p>
            That is the market now: new models, new harnesses, new splits, new
            leaderboards, new claims. The top slot is not a destination. It is a
            timestamp.
          </p>
        </section>

        <section className="blog-section">
          <h2>Why a leaderboard cannot prove SWE is solved</h2>
          <p>
            A benchmark score compresses too much. It hides how many attempts
            were used, whether retrieval was tuned, how tests were selected, how
            failures were retried, whether the task was already familiar, and
            whether the patch would survive a human reviewer who understands the
            product.
          </p>
          <p>
            The LiveBench paper frames one version of the problem directly:
            test set contamination can make benchmarks obsolete, while human or
            LLM judging can introduce bias or break down on hard questions. Its
            proposed answer is frequent updates, objective ground truth, and
            harder tasks over time. That is a good direction. It is also a sign
            that static public benchmarks are fragile by default.
          </p>
          <p>
            The 2025 Stanford AI Index captured the acceleration clearly: on
            SWE-bench, AI systems went from solving 4.4 percent of coding
            problems in 2023 to 71.7 percent in 2024. That is real progress. It
            is also the kind of progress that should make buyers more careful,
            not less careful. When a benchmark moves that fast, it stops being a
            stable proxy for the work you actually need done.
          </p>
        </section>

        <section className="blog-section">
          <h2>The next evaluation unit is the session</h2>
          <p>
            Software engineering is not a prompt. It is a trace. An agent reads
            files, forms hypotheses, edits code, runs commands, interprets
            failures, backs out mistakes, updates tests, asks for context, and
            eventually leaves behind a diff. The best evidence is not only the
            final score. It is the whole path that produced the patch.
          </p>
          <p>
            That is why the next useful benchmark should look less like a
            leaderboard row and more like a reproducible case file:
          </p>
          <ul>
            <li>the original developer session, scrubbed but faithful</li>
            <li>the repository state and issue context</li>
            <li>the agent actions, commands, edits, and failures</li>
            <li>the tests that passed, failed, or were missing</li>
            <li>the reviewer-visible diff and rationale</li>
            <li>the cost, latency, retries, and tool use required to get there</li>
          </ul>
          <p>
            A score still matters. But a score without the session is a
            screenshot of a claim. A session is evidence.
          </p>
        </section>

        <section className="blog-section">
          <h2>What ProofSWE should prove</h2>
          <p>
            The claim should not be &quot;our agent topped the benchmark.&quot; The claim
            should be narrower and stronger: given real coding agent sessions,
            can we turn the work into reproducible software tasks and test
            whether the result survives ambiguity, review, and execution?
          </p>
          <p>
            That means preserving the parts of software work that classic
            benchmarks tend to flatten:
          </p>
          <ul>
            <li>ambiguous intent, not only clean task statements</li>
            <li>repository-specific context, not only isolated functions</li>
            <li>execution traces, not only final answers</li>
            <li>reviewability, not only test pass rate</li>
            <li>freshness, not only a public static split</li>
            <li>failure modes, not only aggregate accuracy</li>
          </ul>
          <p>
            Benchmarks are dead as bragging rights. They are alive as raw
            material. The future is not fewer evaluations. It is evaluations
            that can be replayed.
          </p>
        </section>

      </article>
    </main>
  );
}
