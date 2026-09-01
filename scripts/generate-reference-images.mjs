// Phase F asset harvester: for each catalogued reference, fetch its verdict
// (scope-unknown cell) from the API, take the newest dial-validated receipt
// image, and write a 280×280 webp to public/watches/{brand}/{model}/{REF}.webp.
// Refs without a verdict/receipt image are skipped — frontend falls back to
// receipt → placeholder at runtime.
//
// Usage: node scripts/generate-reference-images.mjs [apiBase]
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

// sharp lives in apps/web/node_modules (Next dependency) — resolve from there.
const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sharp = createRequire(resolve(repoRoot, "apps/web/package.json"))("sharp");

const API = (process.argv[2] || "https://watchfetcher.fly.dev").replace(/\/$/, "");
const CONFIG = "config/models.json";
const OUT_ROOT = resolve(repoRoot, "apps/web/public/watches");

const slugify = (s) =>
  s.toLowerCase().normalize("NFD").replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

const cfg = JSON.parse(await readFile(CONFIG, "utf8"));
const refs = [];
for (const [brand, models] of Object.entries(cfg.references || {})) {
  for (const [model, list] of Object.entries(models)) {
    for (const r of list) refs.push({ brand, model, ref: r.ref });
  }
}
console.log(`harvesting ${refs.length} refs from ${API}`);

let ok = 0, noVerdict = 0, noImage = 0, failed = 0;
const made = [];
for (const { brand, model, ref } of refs) {
  try {
    const res = await fetch(`${API}/api/verdict?ref=${encodeURIComponent(ref)}`);
    if (!res.ok) {
      noVerdict++;
      console.log(`skip ${ref}: no verdict (${res.status})`);
      continue;
    }
    const v = await res.json();
    const candidates = [
      ...(v.hero_image_url ? [v.hero_image_url] : []),
      ...(v.receipts || []).map((r) => r.image_url).filter(Boolean),
    ];
    if (candidates.length === 0) {
      noImage++;
      console.log(`skip ${ref}: verdict but no receipt image`);
      continue;
    }
    let buf = null;
    for (const img of candidates) {
      // Generic source-side placeholders (the1916company "watch.png") are not
      // dial-accurate — worse than falling back at runtime. Skip them.
      if (img.includes("/placeholders/")) continue;
      try {
        const imgRes = await fetch(img, { headers: { "User-Agent": "watchfetcher-assets/1.0" } });
        if (!imgRes.ok) continue;
        const b = Buffer.from(await imgRes.arrayBuffer());
        if (b.length < 5000) continue; // tiny = error placeholder
        buf = b;
        console.log(`ok   ${ref} <- ${img.slice(0, 80)}`);
        break;
      } catch {
        // try next receipt
      }
    }
    if (!buf) {
      failed++;
      console.log(`fail ${ref}: all ${candidates.length} receipt images unreachable`);
      continue;
    }
    const out = `${OUT_ROOT}/${slugify(brand)}/${slugify(model)}/${ref.toUpperCase()}.webp`;
    await mkdir(dirname(out), { recursive: true });
    await sharp(buf)
      .resize(280, 280, { fit: "cover", position: "centre" })
      .webp({ quality: 82 })
      .toFile(out);
    ok++;
    made.push(out);
  } catch (e) {
    failed++;
    console.log(`fail ${ref}: ${e.message}`);
  }
}
await writeFile("scripts/reference-images.log", made.join("\n") + "\n");
console.log(`\ndone: ${ok} written, ${noVerdict} no-verdict, ${noImage} no-image, ${failed} failed`);
