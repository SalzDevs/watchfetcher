// Brand-site harvester: cut real assets from the source.
//  Logos:   {domain}/apple-touch-icon.png (fallback: homepage header logo img) → public/logos/{brand}.png
//  Watches: sitemap.xml → collection URL matching family tokens → page og:image → public/families/{brand}/{model}.png
// Polite: 700ms+ between hits, one-off (rerunnable). PNG/JPEG only — SVG bytes
// rejected (served by extension). Existing family files are overwritten —
// brand-og shots beat Commons.
import { mkdir, writeFile, stat } from "node:fs/promises";
import { readFile } from "node:fs/promises";

const UA = {
  headers: {
    "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36",
    Accept: "text/html,application/xhtml+xml,image/*,*/*;q=0.8",
  },
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const slugify = (s) => s.toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

const BRANDS = {
  Rolex: "https://www.rolex.com",
  "Patek Philippe": "https://www.patek.com",
  "Audemars Piguet": "https://www.audemarspiguet.com",
  "Vacheron Constantin": "https://www.vacheron-constantin.com",
  "Richard Mille": "https://www.richardmille.com",
  Omega: "https://www.omegawatches.com",
  Cartier: "https://www.cartier.com",
  Breitling: "https://www.breitling.com",
  "Jaeger-LeCoultre": "https://www.jaeger-lecoultre.com",
  IWC: "https://www.iwc.com",
  Breguet: "https://www.breguet.com",
  "A. Lange & Söhne": "https://www.alange-soehne.com",
  Tudor: "https://www.tudorwatch.com",
  Hublot: "https://www.hublot.com",
  Panerai: "https://www.panerai.com",
  "TAG Heuer": "https://www.tagheuer.com",
  Longines: "https://www.longines.com",
  "Grand Seiko": "https://grand-seiko.com",
  Chopard: "https://www.chopard.com",
  Bulgari: "https://www.bulgari.com",
};

async function fetchBytes(url) {
  try {
    const r = await fetch(url, UA);
    if (!r.ok) return null;
    const ct = r.headers.get("content-type") || "";
    if (!/image\/(png|jpe?g|webp|x-icon|vnd\.microsoft\.icon)/.test(ct)) return null; // svg/xml rejected
    const b = Buffer.from(await r.arrayBuffer());
    return b.length > 1200 ? b : null;
  } catch { return null; }
}
async function fetchText(url) {
  try {
    const r = await fetch(url, UA);
    if (!r.ok) return null;
    return await r.text();
  } catch { return null; }
}
const ext = (b) => (b[0] === 0x89 ? "png" : b[0] === 0xff ? "jpg" : b[0] === 0x47 ? "gif" : b[0] === 0x42 && b[1] === 0x4d ? "bmp" : null);

const cfg = JSON.parse(await readFile("config/models.json", "utf8"));
await mkdir("apps/web/public/logos", { recursive: true });
await mkdir("apps/web/public/families", { recursive: true });

// ---------- logos ----------
console.log("=== logos ===");
for (const [brand, base] of Object.entries(BRANDS)) {
  let buf = await fetchBytes(`${base}/apple-touch-icon.png`);
  if (!buf) buf = await fetchBytes(`${base}/apple-touch-icon-precomposed.png`);
  let src = "touch-icon";
  if (!buf) {
    const html = await fetchText(base + "/");
    if (html) {
      const m = html.match(/<img[^>]+(?:class|src)="[^"]*logo[^"]*"[^>]*>/i)?.[0] ||
                html.match(/<img[^>]*src="[^"]*logo[^"]*"[^>]*>/i)?.[0];
      const u = m?.match(/src="([^"]+)"/)?.[1];
      if (u) {
        const abs = u.startsWith("http") ? u : base + (u.startsWith("/") ? u : "/" + u);
        buf = await fetchBytes(abs);
        src = "header-img";
      }
    }
  }
  const out = `apps/web/public/logos/${slugify(brand)}.png`;
  const cur = await stat(out).then((s) => s.size).catch(() => 0);
  if (buf) {
    if (ext(buf)) {
      await writeFile(out, buf);
      console.log(`ok   ${brand} <- ${src} (${Math.round(buf.length / 1024)}KB)`);
    } else {
      console.log(`skip ${brand} (non-raster bytes)`);
    }
  } else {
    console.log(cur ? `keep ${brand} (existing ${Math.round(cur / 1024)}KB)` : `miss ${brand}`);
  }
  await sleep(700);
}

// ---------- families ----------
console.log("\n=== families (og:image from collection pages via sitemap) ===");
const sitemapCache = new Map(); // base -> all URLs
async function sitemapURLs(base) {
  if (sitemapCache.has(base)) return sitemapCache.get(base);
  let urls = [];
  const idx = await fetchText(`${base}/sitemap.xml`);
  const children = [...(idx?.matchAll(/<loc>([^<]+)<\/loc>/g) || [])].map((m) => m[1]).filter((u) => u.includes("sitemap") || u.endsWith(".xml"));
  const toFetch = children.length ? children.slice(0, 6) : [`${base}/sitemap.xml`];
  for (const c of toFetch) {
    if (/\.(png|jpe?g|webp|video)\.xml/i.test(c)) continue;
    const t = await fetchText(c);
    if (t) urls.push(...[...t.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1]));
    await sleep(500);
  }
  urls = [...new Set(urls)];
  sitemapCache.set(base, urls);
  return urls;
}

for (const [brand, models] of Object.entries(cfg.brands)) {
  const base = BRANDS[brand];
  if (!base) continue;
  const bLower = brand.toLowerCase();
  const urls = await sitemapURLs(base).catch(() => []);
  if (!urls.length) {
    console.log(`--   ${brand}: no sitemap URLs`);
    continue;
  }
  const dir = `apps/web/public/families/${slugify(brand)}`;
  await mkdir(dir, { recursive: true });
  for (const model of models) {
    const out = `${dir}/${slugify(model)}.png`;
    const tokens = model.toLowerCase().replace(/~|\d+$/g, "").split(/\s+/).filter((t) => t.length > 2);
    if (!tokens.length) continue;
    // match: URL path contains every token (hyphen-insensitive); prefer short collection pages
    const matches = urls.filter((u) => {
      const p = u.toLowerCase();
      return p.startsWith(base.toLowerCase()) && tokens.every((t) => p.includes(t));
    });
    if (!matches.length) {
      console.log(`miss ${brand} / ${model}`);
      continue;
    }
    matches.sort((a, b) => a.length - b.length);
    let done = false;
    for (const page of matches.slice(0, 3)) {
      const html = await fetchText(page);
      await sleep(600);
      if (!html) continue;
      const og = html.match(/<meta[^>]+property="og:image"[^>]+content="([^"]+)"/i)?.[1] ||
                 html.match(/<meta[^>]+content="([^"]+)"[^>]+property="og:image"/i)?.[1];
      if (!og) continue;
      const abs = og.startsWith("http") ? og : base + og;
      const buf = await fetchBytes(abs);
      if (buf) {
        await writeFile(out, buf);
        console.log(`ok   ${brand} / ${model} <- ${page.replace(base, "").slice(0, 50)} (${Math.round(buf.length / 1024)}KB)`);
        done = true;
        break;
      }
    }
    if (!done) console.log(`miss ${brand} / ${model} (no og:image on ${matches.length} candidates)`);
    await sleep(600);
  }
}
console.log("\ndone");
