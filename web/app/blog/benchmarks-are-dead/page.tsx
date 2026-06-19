import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "The Next Unit of AI Evaluation Is the Session, not Benchmarks.",
  description:
    "Every lab tops the benchmark. Real software work still feels hard. ProofSWE is built for the session, not the one shot.",
  alternates: { canonical: "/blog/benchmarks-are-dead" },
  openGraph: {
    title: "The Next Unit of AI Evaluation Is the Session, not Benchmarks.",
    description:
      "Every lab tops the benchmark. Real software work still feels hard. ProofSWE is built for the session.",
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
          <h1>The Next Unit of AI Evaluation Is the Session, not Benchmarks.</h1>
          <p className="blog-dek">
            Every day a new model is released, and every day it somehow tops the
            benchmark. Then you ask it to do real work, and the story gets messy.
          </p>
          <p className="blog-meta">June 19, 2026</p>
        </header>

        <section className="blog-section rise" style={{ animationDelay: "0.1s" }}>
          <p>
            Every day a new model is released, you see them topping the
            benchmarks. Be it Chinese, American, these labs somehow manage to
            top all of them. And that is their marketing moat. Everyone markets
            their models on the basis of those long graphs and lines comparing
            to others, how they are topping everything.
          </p>
          <p>
            But when we come to real life implementation, telling Claude to do
            something, GPT to do something, it is hard. It is not easy at all. It
            often makes mistakes you would never make, and then you think to
            yourself, man why are benchmarks not resonating here? Am I using it
            the dumb way? Do I need more skills, MCPs, and all the LLM jargon?
          </p>
          <p>
            Well, the answer is that the old way to benchmark is stupid, one
            shotted, and does not capture the way we really use these models. It
            captures a clean prompt, a clean answer, a clean score. But real
            software is not clean. It is full of context, taste, broken tests,
            undocumented decisions, weird product constraints, and the very real
            feeling of watching an agent confidently break something you would
            never touch.
          </p>
          <p>
            I want to introduce one, but before that, let&apos;s talk about the
            flaws.
          </p>
        </section>

        <section className="blog-section">
          <h2>The benchmark was supposed to be hard. Then everyone trained for it.</h2>
          <p>
            SWE-bench was important because it moved evaluation from toy code
            completion toward real GitHub issues. The original paper introduced
            2,294 software engineering problems from 12 popular Python
            repositories, and reported that Claude 2 solved only 1.96 percent of
            issues in the initial evaluation. That was the right kind of shock.
            It showed that generating snippets and fixing real repositories are
            different activities.
          </p>
          <p>
            Then, what happened what always happens. These companies and their
            makers want to market their products, and optimising for these
            benchmarks seemed like a good way. So they did it. Half of the
            benchmarks were either in model training corpora, or the models
            gamed them or cheated it. They just passed the tests, and they
            topped it. That was it.
          </p>
          <p>
            But no way this is real life. Software fails for a lot of reasons,
            specially when there is no one to buy it, and it almost always can
            fail even when the tests pass. Passing tests does not mean the way
            to production. A test suite can be incomplete. A product decision
            can be wrong. A migration can work locally and still ruin your day.
            The code can be technically passing and still be something no sane
            engineer wants to own.
          </p>
          <p>
            And the last thing these benchmarks never gave eyes to was
            efficiency. The cost and the time. If the task takes a ridiculous
            amount of time and money to solve it and always requires a human,
            what is the point? If the agent solves one issue after burning
            twenty tool calls, three retries, and your whole afternoon, that is
            not the same as a senior engineer landing a clean patch.
          </p>
        </section>

        <section className="blog-section">
          <h2>Why a leaderboard cannot prove SWE is solved</h2>
          <p>
            I like{" "}
            <a href="https://x.com/bcherny" target="_blank" rel="noreferrer">
              @bcherny
            </a>{" "}
            a lot, he is lowkey one of the smartest guys in the ecosystem. But
            when he said that coding is solved, it felt that it was an
            overstatement. And things started getting proved, slowly. Claude
            Code CLI was the buggiest, Anthropic faced a lot of shortages, and
            software was at an all time high of unreliability. That is not
            called solving coding. I love coding with LLMs, but I am afraid if
            they solved the problem.
          </p>
          <p>
            The LiveBench paper frames one version of the problem directly: test
            set contamination can make benchmarks obsolete, while human or LLM
            judging can introduce bias or break down on hard questions. Its
            proposed answer is frequent updates, objective ground truth, and
            harder tasks over time. That is a good direction. It is also a sign
            that static public benchmarks are fragile by default.
          </p>
          <p>
            The 2025 Stanford AI Index captured the acceleration clearly: on
            SWE-bench, AI systems went from solving 4.4 percent of coding
            problems in 2023 to 71.7 percent in 2024. That is real progress. It
            is also the kind of progress that should make buyers more careful,
            not less careful. The solution? We might already have it.
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
            I swear at Claude so much, almost all the time. No benchmark measure
            this. User&apos;s frustration. It&apos;s actually one of the most
            important things. Being honest, if you will notice, I swear at Codex
            a lot lesser, and I do not know why. There is no way to measure this
            as a benchmark and a public dataset right now.
          </p>
          <p>
            But there should be. Because that frustration is not random. It is a
            signal. It tells you the model is looping. It tells you the model is
            ignoring instructions. It tells you the model is making you become
            the debugger, the product manager, the test runner, and the safety
            system at the same time. The benchmark says solved. The session says
            you are still babysitting.
          </p>
          <p>
            That is the missing unit. Not one prompt. Not one leaderboard score.
            The session. The actual transcript of how the work happened, what
            files changed, what commands ran, what failed, what got fixed, what
            the user had to correct, how long it took, how much it cost, and
            whether the final diff was something you would merge.
          </p>
        </section>

        <section className="blog-section">
          <h2>So what should a real benchmark measure?</h2>
          <p>
            It should measure whether the agent can survive the messy middle of
            software. Not just the final answer. The path. The number of times it
            went in the wrong direction. The number of times it needed a human to
            restate the obvious. The number of times it ran tests without
            understanding the failure. The number of files it touched for no
            reason. The cost of getting to a patch. The time. The frustration.
          </p>
          <p>
            It should also measure whether the work is reproducible. If a model
            claims it solved a task, we should be able to replay the repository
            state, replay the issue, replay the commands, inspect the patch, and
            understand what actually happened. Otherwise the benchmark is just a
            screenshot from a product launch.
          </p>
          <p>
            And it should measure taste. This is the hardest one and maybe the
            most important one. Good engineering is not only passing tests. It is
            knowing what not to touch. It is making the smallest useful change.
            It is respecting the codebase. It is not inventing abstractions
            because the model got bored. It is shipping something that a real
            maintainer can read without wanting to close the laptop.
          </p>
        </section>

        <section className="blog-section">
          <h2>That is what ProofSWE is for</h2>
          <p>
            ProofSWE is not trying to make another shiny leaderboard where
            everyone comes and says they are number one again. The point is to
            take real coding agent sessions and turn them into reproducible
            software tasks. The unit is the work. The trace. The actual
            interaction between a human, an agent, and a repository.
          </p>
          <p>
            If an agent really solved something, prove it. Show the session.
            Show the repo. Show what it changed. Show what it ran. Show where it
            failed. Show how much it cost. Show whether the final thing survives
            review and execution. That is a much better story than a graph that
            says the new model beat the old model by 3 percent on a benchmark
            everyone has already optimized for.
          </p>
          <p>
            Benchmarks are dead because the old benchmark was built for the
            model launch. The new benchmark has to be built for the developer.
            It has to care about the moment where you are sitting there, asking
            the model to fix something real, and wondering why the graph did not
            prepare you for this.
          </p>
          <p>
            That is the gap ProofSWE wants to close. Not by pretending coding is
            solved. By proving, session by session, where it actually is.
          </p>
        </section>
      </article>
    </main>
  );
}
