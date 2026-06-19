import Link from "next/link";
import ThemeToggle from "@/components/theme-toggle";

const GITHUB_URL = "https://github.com/Atharva-Kanherkar/proofswe";

export default function SiteNav() {
  return (
    <header className="site-nav">
      <nav className="site-nav-inner" aria-label="Primary navigation">
        <Link href="/" className="site-brand" aria-label="ProofSWE home">
          <span>ProofSWE</span>
        </Link>

        <div className="site-nav-links">
          <Link href="/leaderboard" className="site-nav-link">
            Leaderboard
          </Link>
          <Link href="/blog" className="site-nav-link">
            Blog
          </Link>
          <a
            href={GITHUB_URL}
            target="_blank"
            rel="noopener noreferrer"
            className="site-nav-link"
          >
            GitHub
          </a>
          <ThemeToggle />
        </div>
      </nav>
    </header>
  );
}
