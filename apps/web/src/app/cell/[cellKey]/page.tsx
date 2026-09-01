import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

const BASE = process.env.NEXT_PUBLIC_API_URL || "https://watchfetcher.fly.dev";

type Verdict = {
  cell_key: string;
  brand: string;
  model: string;
  dial: string;
  material: string;
  scope: string;
  fair_low: number;
  fair_high: number;
  median: number;
  p25: number;
  p75: number;
  count: number;
  computed_at: string;
  hero_image_url?: string;
  receipts: { title: string; price: number; url: string; date: string; source: string; ref: string; image_url?: string }[];
};

async function fetchVerdict(cellKey: string): Promise<Verdict | null> {
  const res = await fetch(`${BASE}/api/verdict?cell_key=${encodeURIComponent(cellKey)}`, { next: { revalidate: 3600 } });
  if (!res.ok) return null;
  return (await res.json()) as Verdict;
}

export const dynamicParams = false;

export async function generateStaticParams() {
  try {
    let offset = 0;
    const limit = 100;
    let total = Infinity;
    const all: { cell_key: string }[] = [];
    while (all.length < total) {
      const res = await fetch(`${BASE}/api/verdicts?limit=${limit}&offset=${offset}&sort=brand_asc`, { next: { revalidate: 3600 } });
      if (!res.ok) break;
      const data = (await res.json()) as { total: number; verdicts: { cell_key: string }[] };
      total = data.total;
      all.push(...data.verdicts);
      offset += limit;
      if (data.verdicts.length === 0) break;
    }
    return all.map((v) => ({ cellKey: v.cell_key }));
  } catch {
    return [];
  }
}

export async function generateMetadata({ params }: { params: Promise<{ cellKey: string }> }): Promise<Metadata> {
  const { cellKey } = await params;
  const key = decodeURIComponent(cellKey);
  const v = await fetchVerdict(key);
  if (!v) return { title: "Not found | WatchFairValue" };
  const title = `${v.brand} ${v.model}${v.dial ? ` ${v.dial}` : ""} — $${v.median.toLocaleString()} fair`;
  const desc = `Exact-cell verdict from ${v.count} identical sales. Fair $${v.fair_low.toLocaleString()}–$${v.fair_high.toLocaleString()} median $${v.median.toLocaleString()} • ${v.dial || "unknown"} dial ${v.material || "unknown"} ${v.scope || ""}`.trim();
  const url = `https://watchfairvalue.fly.dev/cell/${encodeURIComponent(v.cell_key)}`;
  return {
    title,
    description: desc,
    alternates: { canonical: url },
    openGraph: {
      title,
      description: desc,
      url,
      images: v.hero_image_url ? [{ url: v.hero_image_url }] : ["/opengraph-image"],
      type: "website",
    },
    twitter: {
      card: "summary_large_image",
      title,
      description: desc,
      images: v.hero_image_url ? [v.hero_image_url] : ["/opengraph-image"],
    },
  };
}

function fmt(n: number) {
  return new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(n);
}

export default async function CellPage({ params }: { params: Promise<{ cellKey: string }> }) {
  const { cellKey } = await params;
  const key = decodeURIComponent(cellKey);
  const v = await fetchVerdict(key);
  if (!v) notFound();

  const hero = v.hero_image_url || v.receipts.find((r) => r.image_url)?.image_url || "/watches/placeholder.svg";

  return (
    <div className="min-h-screen bg-ink text-enamel">
      <div className="mx-auto max-w-[960px] px-6 md:px-8 py-8">
        <Link href="/" className="inline-flex items-center gap-2 font-mono text-[11px] tracking-[0.12em] uppercase text-steel hover:text-enamel">
          ← Back to caliper
        </Link>

        <div className="mt-6 rounded-[18px] border border-steel/20 bg-enamel text-ink overflow-hidden">
          <div className="grid md:grid-cols-[360px_1fr] gap-0">
            <div className="relative aspect-[4/3] bg-ink overflow-hidden">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img src={hero} alt={`${v.brand} ${v.model} ${v.dial}`} className="h-full w-full object-cover" />
              <div className="absolute left-3 top-3 rounded-full bg-ink/85 backdrop-blur px-2.5 py-1 text-[10px] font-mono tracking-[0.12em] uppercase text-enamel border border-white/10">
                {v.dial || "unknown"} dial
              </div>
            </div>
            <div className="p-6 md:p-8">
              <div className="font-display text-[28px] leading-none tracking-[-0.03em]">
                {v.brand} <span className="italic font-normal">{v.model}</span>
              </div>
              <div className="mt-2 font-mono text-[11px] tracking-[0.12em] uppercase text-ink/60">
                {v.dial || "unknown"} · {v.material || "unknown"} · {v.scope || "unknown"} · {v.count} comps
              </div>
              <div className="mt-6 grid grid-cols-3 gap-4">
                <div>
                  <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-ink/50">Fair low</div>
                  <div className="font-display text-[20px]">{fmt(v.fair_low)}</div>
                </div>
                <div className="text-center">
                  <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-brass">Median</div>
                  <div className="font-display text-[26px]">{fmt(v.median)}</div>
                </div>
                <div className="text-right">
                  <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-ink/50">Fair high</div>
                  <div className="font-display text-[20px]">{fmt(v.fair_high)}</div>
                </div>
              </div>
              <div className="mt-6 rounded-xl bg-ink text-enamel px-4 py-3 font-mono text-[11px] leading-[1.6]">
                Exact-cell only — <span className="text-brass">{v.cell_key}</span> • p25 {fmt(v.p25)} • p75 {fmt(v.p75)} • computed {new Date(v.computed_at).toLocaleDateString("en-GB")}
              </div>
              <a href={`https://watchfetcher.fly.dev/api/verdict?cell_key=${encodeURIComponent(v.cell_key)}`} target="_blank" className="mt-4 inline-flex text-[11px] font-mono tracking-[0.12em] uppercase text-ink/60 hover:text-ink underline decoration-ink/20">
                View JSON →
              </a>
            </div>
          </div>
        </div>

        {/* JSON-LD */}
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{
            __html: JSON.stringify({
              "@context": "https://schema.org",
              "@type": "Product",
              name: `${v.brand} ${v.model} ${v.dial}`.trim(),
              brand: { "@type": "Brand", name: v.brand },
              offers: {
                "@type": "AggregateOffer",
                priceCurrency: "USD",
                lowPrice: v.fair_low,
                highPrice: v.fair_high,
                offerCount: v.count,
              },
            }),
          }}
        />

        <div className="mt-8 rounded-[18px] border border-border bg-[#14181D] overflow-hidden">
          <div className="px-6 py-4 border-b border-border">
            <h2 className="font-mono text-[11px] tracking-[0.14em] uppercase text-steel">Evidence — {v.receipts.length} receipts</h2>
          </div>
          <div className="divide-y divide-border">
            {v.receipts.map((r) => (
              <a key={r.url} href={r.url} target="_blank" className="flex items-center gap-4 px-6 py-3.5 hover:bg-white/[0.02]">
                <div className="h-12 w-12 rounded-[10px] overflow-hidden border border-white/10 bg-ink shrink-0">
                  {/* eslint-disable-next-line @next/next/no-img-element */}
                  <img src={r.image_url || "/watches/placeholder.svg"} alt={r.title} className="h-full w-full object-cover" loading="lazy" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="truncate font-mono text-[12px] text-enamel">{r.title}</div>
                  <div className="font-mono text-[10px] tracking-[0.06em] text-patina">{r.source} • {r.date} • {r.ref || "no ref"}</div>
                </div>
                <div className="font-mono text-[13px] text-enamel">{fmt(r.price)}</div>
              </a>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
