import type { Metadata } from "next";
import { Geist, Geist_Mono, Instrument_Serif } from "next/font/google";
import "./globals.css";

// Body / UI / data — technical precision
const sans = Geist({
  variable: "--font-sans-stack",
  subsets: ["latin"],
  display: "swap",
});

// Figures / labels / readouts
const mono = Geist_Mono({
  variable: "--font-mono-stack",
  subsets: ["latin"],
  display: "swap",
});

// Display + editorial accents (incl. italic)
const serif = Instrument_Serif({
  variable: "--font-serif-accent",
  subsets: ["latin"],
  weight: "400",
  style: ["normal", "italic"],
  display: "swap",
});

const SITE = "https://proofswe.com";
const TITLE = "ProofSWE — benchmarks are dead. proof is not.";
const DESC =
  "Free yourself from benchmark pain. ProofSWE scores coding agents on real developer sessions — cost, tools, merges, the whole transcript. An AgentClash joint.";

export const metadata: Metadata = {
  metadataBase: new URL(SITE),
  title: TITLE,
  description: DESC,
  keywords: [
    "ProofSWE",
    "coding agent benchmark",
    "SWE benchmark",
    "AI agent evaluation",
    "AgentClash",
    "developer benchmark",
  ],
  alternates: { canonical: "/" },
  openGraph: {
    title: TITLE,
    description: DESC,
    url: SITE,
    siteName: "ProofSWE",
    type: "website",
  },
  twitter: {
    card: "summary_large_image",
    title: TITLE,
    description: DESC,
  },
  robots: { index: true, follow: true },
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      className={`${sans.variable} ${mono.variable} ${serif.variable} h-full antialiased`}
    >
      <body className="min-h-full">{children}</body>
    </html>
  );
}
