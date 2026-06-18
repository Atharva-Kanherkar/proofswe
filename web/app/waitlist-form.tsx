"use client";

import { useState } from "react";

type Status = "idle" | "loading" | "done" | "error";

export default function WaitlistForm() {
  const [email, setEmail] = useState("");
  const [status, setStatus] = useState<Status>("idle");
  const [msg, setMsg] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (status === "loading") return;
    setStatus("loading");
    setMsg("");
    try {
      const res = await fetch("/api/waitlist", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email }),
      });
      const data = (await res.json().catch(() => ({}))) as {
        error?: string;
      };
      if (!res.ok) throw new Error(data.error || "something broke. try again.");
      setStatus("done");
    } catch (err) {
      setStatus("error");
      setMsg(err instanceof Error ? err.message : "something broke. try again.");
    }
  }

  if (status === "done") {
    return (
      <p className="font-mono text-sm tracking-wide text-[var(--fg)]">
        you&apos;re in. <span className="text-[var(--muted)]">welcome to the proof.</span>
      </p>
    );
  }

  return (
    <form onSubmit={submit} className="w-full max-w-md">
      <div className="flex items-center gap-3 border-b border-[var(--line)] pb-2 transition-colors focus-within:border-[var(--fg)]">
        <input
          type="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          placeholder="your@email.com"
          aria-label="email address"
          className="w-full bg-transparent font-mono text-base text-[var(--fg)] placeholder:text-[var(--muted)] outline-none"
        />
        <button
          type="submit"
          disabled={status === "loading"}
          className="shrink-0 font-mono text-sm tracking-wider text-[var(--fg)] transition-opacity hover:opacity-60 disabled:opacity-40"
        >
          {status === "loading" ? "..." : "lock in →"}
        </button>
      </div>
      {status === "error" && (
        <p className="mt-2 font-mono text-xs text-red-400/80">{msg}</p>
      )}
    </form>
  );
}
