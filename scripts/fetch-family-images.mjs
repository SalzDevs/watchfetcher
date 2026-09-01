// Fetches representative family images from Wikimedia Commons search.
// apps/web/public/families/{brand-slug}/{model-slug}.png — used as the middle
// fallback in the model tile chain: receipt hero → curated ref art → family
// image (this) → monogram.
import { mkdir, writeFile } from "node:fs/promises";
import { stat } from "node:fs/promises";
import { readFile } from "node:fs/promises";

const UA = { headers: { "User-Agent": "watchfetcher-assets/1.0 (family imagery)" } };
const cfg = JSON.parse(await readFile("config/models.json", "utf8"));

const slugify = (s) =>
  s.toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

async function f(url) {
  try {
    const r = await fetch(url, UA);
    if (!r.ok || !r.headers.get("content-type")?.startsWith("image")) return null;
    const b = Buffer.from(await r.arrayBuffer());
    return b.length > 3000 ? b : null;
  } catch { return null; }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
let ok = 0, miss = 0;
for (const [brand, models] of Object.entries(cfg.brands)) {
  const dir = `apps/web/public/families/${slugify(brand)}`;
  await mkdir(dir, { recursive: true });
  for (const model of models) {
    const q = `${brand} ${model} watch`;
    const api = `https://commons.wikimedia.org/w/api.php?action=query&generator=search&gsrsearch=${encodeURIComponent(q)}&gsrnamespace=6&gsrlimit=4&prop=imageinfo&iiprop=url&iiurlwidth=420&format=json`;
    let buf = null;
    try {
      const out = `${dir}/${slugify(model)}.png`;
      try { await stat(out); ok++; console.log(`skip ${brand} / ${model} (exists)`); continue; } catch {}
      const r = await fetch(api, UA);
      if (r.status === 429) { await sleep(8000); continue; }
      if (r.ok) {
        const j = await r.json();
        const pages = Object.values(j.query?.pages || {});
        for (const p of pages) {
          const u = p.imageinfo?.[0]?.thumburl || p.imageinfo?.[0]?.url;
          if (!u) continue;
          buf = await f(u);
          if (buf) break;
        }
      }
    } catch {}
    if (buf) {
      await writeFile(`${dir}/${slugify(model)}.png`, buf);
      ok++;
      console.log(`ok   ${brand} / ${model}`);
    } else {
      miss++;
      console.log(`miss ${brand} / ${model}`);
    }
    await sleep(1800); // polite — commons anon search limit is low
  }
}
console.log(`\ndone: ${ok} family images, ${miss} missing (monogram fallback)`);
