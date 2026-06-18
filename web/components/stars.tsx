// Deterministic starfield — positions computed once from a seeded PRNG so
// server and client render identically (no hydration mismatch, no Math.random).
const STARS = (() => {
  let seed = 1337;
  const rand = () => {
    seed = (seed * 1103515245 + 12345) & 0x7fffffff;
    return seed / 0x7fffffff;
  };
  return Array.from({ length: 80 }, () => ({
    left: rand() * 100,
    top: rand() * 92,
    size: rand() * 1.6 + 0.6,
    delay: rand() * 5,
    dur: rand() * 3 + 2.5,
  }));
})();

export default function Stars() {
  return (
    <div className="stars" aria-hidden="true">
      {STARS.map((s, i) => (
        <span
          key={i}
          style={{
            left: `${s.left}%`,
            top: `${s.top}%`,
            width: `${s.size}px`,
            height: `${s.size}px`,
            animationDelay: `${s.delay}s`,
            animationDuration: `${s.dur}s`,
          }}
        />
      ))}
    </div>
  );
}
