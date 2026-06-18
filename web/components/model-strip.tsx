import type { CSSProperties } from "react";

const LOBE_COLOR =
  "https://unpkg.com/@lobehub/icons-static-svg@latest/icons";

const MODELS = [
  {
    name: "Claude",
    src: `${LOBE_COLOR}/claude-color.svg`,
    glow: "#d97757",
  },
  {
    name: "GPT",
    src: "https://upload.wikimedia.org/wikipedia/commons/0/04/ChatGPT_logo.svg",
    glow: "#74aa9c",
  },
  {
    name: "Gemini",
    src: `${LOBE_COLOR}/gemini-color.svg`,
    glow: "#4285f4",
  },
  {
    name: "Z.AI",
    src: "https://z-cdn.chatglm.cn/z-ai/static/logo.svg",
    glow: "#6b8cff",
  },
  {
    name: "Kimi",
    src: `${LOBE_COLOR}/kimi-color.svg`,
    glow: "#1783ff",
  },
  {
    name: "Composer",
    src: "https://www.cursor.com/brand/icon.svg",
    glow: "#4faaff",
  },
  {
    name: "DeepSeek",
    src: `${LOBE_COLOR}/deepseek-color.svg`,
    glow: "#4d6bfe",
  },
  {
    name: "Grok",
    src: "https://grok.com/icon-512x512.png",
    glow: "#e8e8e8",
  },
] as const;

export default function ModelStrip() {
  return (
    <section
      className="rise model-strip"
      style={{ animationDelay: "0.95s" }}
      aria-label="AI models topping benchmarks"
    >
      <div className="model-strip-grid">
        {MODELS.map(({ name, src, glow }) => (
          <div
            key={name}
            className="model-strip-cell"
            style={{ "--cell-glow": glow } as CSSProperties}
          >
            <div className="model-strip-logo-area">
              <div className="model-strip-glow" aria-hidden="true" />
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src={src} alt="" className="model-strip-logo" />
            </div>
            <span className="model-strip-name">{name}</span>
          </div>
        ))}
      </div>

      <p className="model-strip-caption">
        all of them topping the benchmarks.{" "}
        <span className="text-[var(--fg)]">
          so why are we still struggling to code?
        </span>
      </p>
    </section>
  );
}
