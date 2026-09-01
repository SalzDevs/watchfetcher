// Fetches brand logos into apps/web/public/logos/{slug}.png.
// Source chain per brand: Wikipedia REST summary lead image (usually the
// official mark) → Clearbit logo API → Google favicon @128. Files <2KB or
// failed → skipped (UI falls back to wordmark monogram).
import { mkdir, writeFile, stat } from "node:fs/promises";

const OUT = "apps/web/public/logos";
const WIKI = {
  rolex: "Rolex",
  "patek-philippe": "Patek Philippe",
  "audemars-piguet": "Audemars Piguet",
  "vacheron-constantin": "Vacheron Constantin",
  "richard-mille": "Richard Mille",
  omega: "Omega SA",
  cartier: "Cartier",
  breitling: "Breitling SA",
  "jaeger-lecoultre": "Jaeger-LeCoultre",
  iwc: "IWC Schaffhausen",
  breguet: "Breguet (brand)",
  "a-lange-soehne": "A. Lange & Söhne",
  tudor: "Tudor (watch brand)",
  hublot: "Hublot",
  panerai: "Panerai",
  "tag-heuer": "TAG Heuer",
  longines: "Longines",
  "grand-seiko": "Grand Seiko",
  chopard: "Chopard",
  bulgari: "Bulgari",
};
const DOMAINS = {
  rolex: "rolex.com", "patek-philippe": "patek.com", "audemars-piguet": "audemarspiguet.com",
  "vacheron-constantin": "vacheron-constantin.com", "richard-mille": "richardmille.com",
  omega: "omegawatches.com", cartier: "cartier.com", breitling: "breitling.com",
  "jaeger-lecoultre": "jaeger-lecoultre.com", iwc: "iwc.com", breguet: "breguet.com",
  "a-lange-soehne": "alange-soehne.com", tudor: "tudorwatch.com", hublot: "hublot.com",
  panerai: "panerai.com", "tag-heuer": "tagheuer.com", longines: "longines.com",
  "grand-seiko": "grand-seiko.com", chopard: "chopard.com", bulgari: "bulgari.com",
};

const UA = { headers: { "User-Agent": "watchfetcher-assets/1.0 (brand logo fetch)" } };

async function tryFetch(url, accept) {
  try {
    const res = await fetch(url, { ...UA, headers: { ...UA.headers, ...(accept ? { Accept: accept } : {}) } });
    if (!res.ok) return null;
    const buf = Buffer.from(await res.arrayBuffer());
    if (buf.length < 2000) return null; // 1x1 / error stubs
    return buf;
  } catch {
    return null;
  }
}

await mkdir(OUT, { recursive: true });
let ok = 0, fallback = 0, failed = 0;
for (const [slug, wiki] of Object.entries(WIKI)) {
  const out = `${OUT}/${slug}.png`;
  let buf = null, src = "";

  // 1. Wikipedia lead image (SVG originals preferred for crisp marks)
  try {
    const res = await fetch(`https://en.wikipedia.org/api/rest_v1/page/summary/${encodeURIComponent(wiki)}`, UA);
    if (res.ok) {
      const j = await res.json();
      const u = j.originalimage?.source || j.thumbnail?.source;
      if (u) buf = await tryFetch(u.replace(/\/\d+px-/, "/480px-")) || await tryFetch(u);
      if (buf) src = "wikipedia";
    }
  } catch {}
  // 2. Clearbit
  if (!buf) {
    buf = await tryFetch(`https://logo.clearbit.com/${DOMAINS[slug]}?size=256`);
    if (buf) src = "clearbit";
  }
  // 3. Google favicon
  if (!buf) {
    buf = await tryFetch(`https://www.google.com/s2/favicons?domain=${DOMAINS[slug]}&sz=128`);
    if (buf) src = "favicon";
  }

  if (!buf) {
    failed++;
    console.log(`miss ${slug}`);
    continue;
  }
  await writeFile(out, buf);
  const size = (await stat(out)).size;
  ok++;
  if (src !== "wikipedia") fallback++;
  console.log(`ok   ${slug} <- ${src} (${Math.round(size / 1024)}KB)`);
}
console.log(`\ndone: ${ok} logos (${fallback} via fallback source), ${failed} missing`);
