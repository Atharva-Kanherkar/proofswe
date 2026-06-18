# Design System — ProofSWE

## Product Context
- **What this is:** An open, multi-signal benchmark + corpus for coding agents, built from real developer sessions. Turns a Codex/Claude Code transcript into a reproducible task, scores it against a hosted judge, returns an official scorecard.
- **Who it's for:** AI researchers, agent builders, and ML/dev-tool teams who need a credible read on coding-agent quality.
- **Space/industry:** AI evaluation / benchmarks (lineage: SWE-bench, τ-bench, LMArena, Epoch AI).
- **Project type:** Marketing site + waitlist now; leaderboard, frontier charts, and blog later.
- **Affiliation:** An AgentClash project.

## Memorable Thing
**Rigorous + inevitable.** A frontier instrument at night — precise, confident, quietly cinematic. Every choice serves this. Hype comes from gravity, not loudness.

## Aesthetic Direction
- **Direction:** Observatory — dark editorial / scientific.
- **Decoration level:** Intentional. Faint starfield + a single hairline horizon + fine grain, on marketing surfaces only. Data surfaces stay clean.
- **Mood:** The quiet confidence of a frontier lab after dark. Spacious, high-contrast, restrained.
- **Category insight:** Peers (SWE-bench, LMArena, Epoch, Artificial Analysis) earn trust by looking like a 2015 paper — credible but forgettable. ProofSWE keeps every rigor signal (confidence intervals, frontier scatter, methodology, org logos) and wraps it in an aesthetic none of them dare to use.

## Typography
- **Display / editorial accent:** Instrument Serif (400, + italic) — gravitas no competitor has; the italic carries accent words (e.g. "*proof* is not").
- **Body / UI / data:** Geist — technical precision; supports tabular figures for tables.
- **Figures / labels / readouts:** Geist Mono — scores, ±CI, tracked uppercase tags.
- **Loading:** `next/font/google` (Geist, Geist_Mono, Instrument_Serif), `display: swap`.
- **Scale (clamp, fluid):**
  - Wordmark: `clamp(2.5rem, 8vw, 5rem)`, Geist 500, tracking -0.02em
  - Editorial headline: `clamp(1.3rem, 3.4vw, 2.1rem)`, leading 1.2
  - Body: 16px / 1.6
  - Subtitle / muted: 14–16px
  - Mono label: 11px, uppercase, tracking 0.22em
- **Never use:** Inter, Space Grotesk, Roboto, system-ui as display (the convergence trap).

## Color
- **Approach:** Restrained. Neutrals do the work; one chromatic accent, used rarely.
- **Canvas:** `#060709` (cool near-black)
- **Surface:** `#0E1014` (cards, leaderboard rows)
- **Surface raised / hover:** `#161922`
- **Hairline border:** `rgba(255,255,255,0.10)`
- **Text primary:** `#F4F5F7`
- **Text muted:** `#8A8F98`
- **Text faint:** `#5A5F68`
- **Accent — "proof magenta":** `#E85DDA` (refined, not neon). Used only for: the accent word, active/top rank, focus rings, a single dot. No peer in evals owns pink.
- **Semantic (data only):** success `#3FB37F`, warning `#E0A23C`, error `#E2555A`, info `#5AA0E2`.
- **Dark mode:** This IS the design. (No light mode for v1.)

## Spacing
- **Base unit:** 8px.
- **Density:** Spacious on marketing, compact on data tables.
- **Scale:** 2xs(2) xs(4) sm(8) md(16) lg(24) xl(32) 2xl(48) 3xl(64) 4xl(96)

## Layout
- **Approach:** Hybrid. Marketing = creative-editorial, centered, cinematic, generous negative space. Leaderboard/app = grid-disciplined, dense-but-clean.
- **Max content width:** 1120px (data), 640px (editorial column).
- **Border radius:** sm 6px, md 10px, lg 14px, pill 9999px.
- **Signature viz:** cost-vs-capability frontier scatter (the Pareto plot). Confidence intervals shown on every score.

## Motion
- **Approach:** Intentional, restrained. "Confident stillness."
- **Easing:** enter `cubic-bezier(0.22,1,0.36,1)`, move ease-in-out.
- **Duration:** micro 80ms, short 200ms, medium 350ms, long 600ms.
- **Patterns:** staggered entrance fade/rise on hero; data settles once; faint star twinkle. No scroll choreography. `prefers-reduced-motion` fully respected.

## Brand Assets
- **Wordmark:** "ProofSWE" set in Geist 500, off-white, tight tracking. Precise, technical — not chrome, not serif.
- **Mascot:** none in v1 (the magenta butterfly was retired for a purely typographic/data identity).

## Decisions Log
| Date | Decision | Rationale |
|------|----------|-----------|
| 2026-06-18 | Initial design system ("Observatory") | Created by /design-consultation. Resolves the credible-vs-hype tension; differentiates hard from a bland peer set. |
| 2026-06-18 | Dropped Space Grotesk chrome wordmark | Overused / convergence trap; chrome read as Y2K. Replaced with Geist + Instrument Serif editorial accent. |
| 2026-06-18 | Retired the magenta butterfly | User chose a purely typographic/data identity for maximum seriousness. Color heritage kept as the accent. |
