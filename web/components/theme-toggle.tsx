"use client";

import { useEffect, useState } from "react";

type Theme = "light" | "dark";

export default function ThemeToggle() {
  // Mirrors the data-theme set by the no-flash script in layout.tsx.
  const [theme, setTheme] = useState<Theme>("light");

  useEffect(() => {
    // Sync React state to the theme the no-flash script already applied.
    // Intentional setState-on-mount to avoid an SSR/client hydration mismatch.
    const current = document.documentElement.dataset.theme as Theme | undefined;
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (current) setTheme(current);
  }, []);

  function toggle() {
    const next: Theme = theme === "light" ? "dark" : "light";
    document.documentElement.dataset.theme = next;
    try {
      localStorage.setItem("theme", next);
    } catch {
      // ignore — private mode / storage disabled
    }
    setTheme(next);
  }

  const nextLabel = theme === "light" ? "dark" : "light";

  return (
    <button
      type="button"
      onClick={toggle}
      className="theme-toggle"
      aria-label={`Switch to ${nextLabel} theme`}
      title={`Switch to ${nextLabel} theme`}
    >
      {theme === "light" ? <MoonIcon /> : <SunIcon />}
    </button>
  );
}

function MoonIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79Z"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function SunIcon() {
  return (
    <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <circle cx="12" cy="12" r="4.2" stroke="currentColor" strokeWidth="1.6" />
      <path
        d="M12 2.5v2.2M12 19.3v2.2M21.5 12h-2.2M4.7 12H2.5M18.7 5.3l-1.6 1.6M6.9 17.1l-1.6 1.6M18.7 18.7l-1.6-1.6M6.9 6.9 5.3 5.3"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
    </svg>
  );
}
