import { ImageResponse } from "next/og";
import { loadFraunces } from "@/lib/og-font";

export const alt = "ProofSWE. benchmarks are dead. proof is not.";
export const size = { width: 1200, height: 630 };
export const contentType = "image/png";

export default async function Image() {
  const [display, body] = await Promise.all([
    loadFraunces(600),
    loadFraunces(400),
  ]);

  const fonts = [
    display && {
      name: "Fraunces",
      data: display,
      weight: 600 as const,
      style: "normal" as const,
    },
    body && {
      name: "Fraunces",
      data: body,
      weight: 400 as const,
      style: "normal" as const,
    },
  ].filter(Boolean) as {
    name: string;
    data: ArrayBuffer;
    weight: 400 | 600;
    style: "normal";
  }[];

  const fontFamily = fonts.length ? "Fraunces" : "serif";

  return new ImageResponse(
    (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          padding: "72px 80px",
          background: "#f6f1e7",
          color: "#1f1a14",
          fontFamily,
        }}
      >
        <div style={{ display: "flex" }} />

        <div style={{ display: "flex", flexDirection: "column" }}>
          <div style={{ display: "flex", fontSize: 168, fontWeight: 600, lineHeight: 1 }}>
            ProofSWE
          </div>
          <div
            style={{
              display: "flex",
              fontSize: 52,
              fontWeight: 400,
              marginTop: 24,
              color: "#3a342b",
            }}
          >
            benchmarks are dead. proof is not.
          </div>
        </div>

        <div style={{ display: "flex", fontSize: 26, color: "#6a6253" }}>
          launching soon · agentclash.dev
        </div>
      </div>
    ),
    { ...size, fonts },
  );
}
