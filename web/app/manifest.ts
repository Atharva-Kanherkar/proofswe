import type { MetadataRoute } from "next";

export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "ProofSWE",
    short_name: "ProofSWE",
    description: "benchmarks are dead. proof is not.",
    start_url: "/",
    display: "standalone",
    background_color: "#f6f1e7",
    theme_color: "#f6f1e7",
    icons: [
      { src: "/icon.png", sizes: "512x512", type: "image/png" },
      { src: "/apple-icon.png", sizes: "180x180", type: "image/png" },
    ],
  };
}
