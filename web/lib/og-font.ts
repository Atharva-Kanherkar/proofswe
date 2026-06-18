// Best-effort loader for the Fraunces display face used in generated
// social/icon images. Returns null on any failure so image generation
// falls back to the default face instead of breaking the build.
export async function loadFraunces(
  weight: number,
  text?: string,
): Promise<ArrayBuffer | null> {
  try {
    const family = `Fraunces:opsz,wght@9..144,${weight}`;
    const params = new URLSearchParams({ family });
    if (text) params.set("text", text);
    const cssUrl = `https://fonts.googleapis.com/css2?${params.toString()}`;

    const css = await fetch(cssUrl, {
      headers: {
        // Old UA so Google serves a TTF/WOFF that Satori can parse (not WOFF2).
        "User-Agent":
          "Mozilla/5.0 (compatible; MSIE 9.0; Windows NT 6.1; Trident/5.0)",
      },
    }).then((r) => r.text());

    const url = css.match(/src:\s*url\(([^)]+?)\)/)?.[1];
    if (!url) return null;

    const res = await fetch(url);
    if (!res.ok) return null;
    return await res.arrayBuffer();
  } catch {
    return null;
  }
}
