import type { Metadata, Viewport } from "next";
import { Fraunces, Geist_Mono } from "next/font/google";
import { Analytics } from "@vercel/analytics/next";
import SiteNav from "@/components/site-nav";
import "./globals.css";

// Runs before paint so the stored/system theme is applied with no flash.
const THEME_INIT = `(function(){try{var t=localStorage.getItem('theme');if(t!=='light'&&t!=='dark'){t=window.matchMedia('(prefers-color-scheme: dark)').matches?'dark':'light';}document.documentElement.dataset.theme=t;}catch(e){document.documentElement.dataset.theme='light';}})();`;

const serif = Fraunces({
  variable: "--font-serif-stack",
  subsets: ["latin"],
  display: "swap",
});

const mono = Geist_Mono({
  variable: "--font-mono-stack",
  subsets: ["latin"],
  display: "swap",
});

const SITE = "https://proofswe.com";
const DESC =
  "benchmarks are dead. proof is not. ProofSWE scores coding agents on real developer sessions, not toy tests. Launching soon.";

export const metadata: Metadata = {
  metadataBase: new URL(SITE),
  title: {
    default: "ProofSWE",
    template: "%s · ProofSWE",
  },
  description: DESC,
  applicationName: "ProofSWE",
  keywords: [
    "ProofSWE",
    "coding agent benchmark",
    "SWE benchmark",
    "AI agent evaluation",
    "AgentClash",
    "developer benchmark",
  ],
  authors: [{ name: "AgentClash", url: "https://agentclash.dev" }],
  creator: "AgentClash",
  alternates: { canonical: "/" },
  openGraph: {
    title: "ProofSWE",
    description: DESC,
    url: SITE,
    siteName: "ProofSWE",
    type: "website",
    locale: "en_US",
  },
  twitter: {
    card: "summary_large_image",
    title: "ProofSWE",
    description: DESC,
    creator: "@agentclash",
  },
  robots: {
    index: true,
    follow: true,
    googleBot: { index: true, follow: true, "max-image-preview": "large" },
  },
  category: "technology",
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f6f1e7" },
    { media: "(prefers-color-scheme: dark)", color: "#15120c" },
  ],
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${serif.variable} ${mono.variable} h-full antialiased`}
    >
      <body className="min-h-full">
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT }} />
        <SiteNav />
        {children}
        <div className="creator-credit">
          <a
            href="https://x.com/attharrva15"
            target="_blank"
            rel="noopener noreferrer"
          >
            follow the creator atharva ↗
          </a>
          <a
            href="https://x.com/AgentClashDev"
            target="_blank"
            rel="noopener noreferrer"
          >
            follow agentclash ↗
          </a>
        </div>
        <Analytics />
      </body>
    </html>
  );
}
