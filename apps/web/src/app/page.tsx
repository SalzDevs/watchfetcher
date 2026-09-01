"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { motion, AnimatePresence } from "framer-motion";
import NumberFlow, { NumberFlowGroup } from "@number-flow/react";
import { Virtuoso } from "react-virtuoso";
import { toast } from "sonner";
import { Command } from "cmdk";
import { clsx } from "clsx";
import { cva } from "class-variance-authority";
import { api, Verdict, RefComps, RefLookup, HistoryPoint } from "@/lib/api";
import { useFilters } from "@/lib/store";
import { DIAL_SWATCHES, UNKNOWN_SWATCH, MATERIAL_LABELS, curatedImagePath } from "@/lib/reference";
import { VaultPicker } from "@/components/VaultPicker";

const PLACEHOLDER = "/watches/placeholder.svg";

const springDefault = { type: "spring" as const, stiffness: 380, damping: 38, mass: 1 };
const springBouncy = { type: "spring" as const, stiffness: 380, damping: 28, mass: 1 };
const springGentle = { type: "spring" as const, stiffness: 300, damping: 30, mass: 1 };

// --- helpers ---
const fmtUSD = (n: number) =>
  new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 0,
  }).format(n);

const chip = cva(
  "inline-flex items-center rounded-full border px-3 py-1.5 text-[11px] tracking-[0.14em] uppercase cursor-pointer select-none will-transform active:scale-[0.97] transition-[transform,background,border,color] duration-[100ms]",
  {
    variants: {
      active: {
        true: "bg-brass text-ink border-brass shadow-[0_2px_10px_rgba(193,154,91,0.35)]",
        false: "bg-transparent text-steel border-border hover:border-border-strong hover:text-enamel hover:bg-white/[0.02]",
      },
    },
    defaultVariants: { active: false },
  }
);


// Caliper with scrub
function Caliper({ verdict }: { verdict: Verdict }) {
  const pad = (verdict.p75 - verdict.p25) * 0.22 || verdict.median * 0.08;
  const min = Math.max(0, verdict.p25 - pad);
  const max = verdict.p75 + pad;
  const span = max - min || 1;
  const pct = (v: number) => ((v - min) / span) * 100;
  const p25 = pct(verdict.p25);
  const p75 = pct(verdict.p75);
  const med = pct(verdict.median);

  const [scrubPct, setScrubPct] = useState<number | null>(null);
  const displayPct = scrubPct ?? med;
  const displayPrice = min + (span * displayPct) / 100;

  return (
    <div className="relative select-none">
      <div
        className="relative h-[42px] rounded-full border border-steel/25 bg-gradient-to-b from-white/[0.04] to-white/[0.01] overflow-hidden will-transform"
        onPointerDown={(e) => {
          const rect = (e.currentTarget as HTMLDivElement).getBoundingClientRect();
          const update = (clientX: number) => {
            const p = ((clientX - rect.left) / rect.width) * 100;
            setScrubPct(Math.max(0, Math.min(100, p)));
          };
          update(e.clientX);
          const move = (ev: PointerEvent) => update(ev.clientX);
          const up = () => {
            window.removeEventListener("pointermove", move);
            window.removeEventListener("pointerup", up);
            setTimeout(() => setScrubPct(null), 800);
          };
          window.addEventListener("pointermove", move);
          window.addEventListener("pointerup", up);
          (e.currentTarget as HTMLDivElement).setPointerCapture((e as any).pointerId);
        }}
      >
        <div className="absolute inset-[7px] rounded-full bg-ink/90 border border-white/5" />
        <motion.div
          layoutId="caliper-band"
          initial={{ scaleX: 0 }}
          animate={{ scaleX: 1 }}
          transition={{ ...springDefault, delay: 0.12 }}
          className="absolute top-[7px] bottom-[7px] bg-brass origin-left will-transform"
          style={{ left: `${p25}%`, width: `${p75 - p25}%` }}
        />
        <motion.div
          layoutId="caliper-needle"
          animate={{ left: `${displayPct}%` }}
          transition={springBouncy}
          className="absolute top-0 bottom-0 w-[2px] bg-ink -ml-px will-transform"
        >
          <div className="absolute -top-1 left-1/2 -translate-x-1/2 h-3 w-3 rotate-45 bg-brass border border-ink shadow-[0_1px_6px_rgba(0,0,0,0.4)]" />
          <div className="absolute -bottom-1 left-1/2 -translate-x-1/2 h-3 w-3 rotate-45 bg-brass border border-ink" />
        </motion.div>
        {verdict.receipts.slice(0, 8).map((r, i) => {
          const x = pct(r.price);
          if (x < 0 || x > 100) return null;
          return (
            <motion.div
              key={r.url + i}
              initial={{ opacity: 0, scaleY: 0 }}
              animate={{ opacity: 1, scaleY: 1 }}
              transition={{ ...springGentle, delay: 0.35 + i * 0.03 }}
              className="absolute top-[7px] bottom-[7px] w-px bg-white/35 origin-center will-transform"
              style={{ left: `${x}%` }}
              title={`${r.source} ${fmtUSD(r.price)}`}
            />
          );
        })}
        <div className="absolute top-1/2 -translate-y-1/2 left-[7px] h-5 w-1 rounded-full bg-steel" />
        <div className="absolute top-1/2 -translate-y-1/2 right-[7px] h-5 w-1 rounded-full bg-steel" />
      </div>
      <div className="mt-2 flex justify-between font-mono text-[10px] tracking-[0.08em] text-ink/50">
        <span>{fmtUSD(min)}</span>
        <motion.span animate={{ scale: scrubPct !== null ? 1.08 : 1 }} className="text-brass">
          {scrubPct !== null ? fmtUSD(displayPrice) : `median ${fmtUSD(verdict.median)}`}
        </motion.span>
        <span>{fmtUSD(max)}</span>
      </div>
    </div>
  );
}

export default function Page() {
  const { filters, set, resetDialMaterialScope } = useFilters();
  const router = useRouter();
  const [comps, setComps] = useState<RefComps | null>(null);
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  const [refLookup, setRefLookup] = useState<RefLookup | null>(null);
  const [cmdInput, setCmdInput] = useState("");
  const [verdict, setVerdict] = useState<Verdict | null>(null);
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [openCmd, setOpenCmd] = useState(false);
  const [directory, setDirectory] = useState<{ total: number; verdicts: any[] } | null>(null);

  useEffect(() => {
    const q = new URLSearchParams(window.location.search);
    const cell = q.get("cell_key");
    if (cell) {
      const parts = cell.split("|");
      set({ brand: parts[0] || "", model: parts[1] || "", ref: "", dial: parts[2] || "", material: parts[3] || "", scope: parts[4] || "" });
    } else if (q.get("brand") || q.get("model")) {
      set({
        brand: q.get("brand") || filters.brand,
        model: q.get("model") || filters.model,
        ref: "",
        dial: q.get("dial") || "",
        material: q.get("material") || "",
        scope: q.get("scope") || "",
      });
    }
  }, []);

  useEffect(() => {
    const params = new URLSearchParams();
    params.set("cell_key", [filters.brand, filters.model, filters.dial, filters.material, filters.scope].map((s) => s.trim().toLowerCase()).join("|"));
    router.replace(`?${params.toString()}`, { scroll: false });
  }, [filters, router]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpenCmd((v) => !v);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Reference picker: catalogued SKUs for the selected family. Empty for
  // vintage / uncatalogued models → free dial/material pickers stay as fallback.
  // (Moved into VaultPicker — this effect kept for modelsByBrand used by ⌘K.)

  useEffect(() => {
    let cancelled = false;
    if (!filters.brand || !filters.model) {
      // Nothing picked yet — no verdict to measure, stay quiet.
      setVerdict(null);
      setErr(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    setErr(null);
    api
      .verdict(filters)
      .then((v) => {
        if (!cancelled) setVerdict(v);
      })
      .catch((e: any) => {
        if (e.status === 404) {
          setVerdict(null);
          setErr(e.message || "No verdict — try unknown");
        } else if (e.status === 400) {
          setErr(e.message);
          setVerdict(null);
        } else {
          setErr(e.message || "Failed to load");
          setVerdict(null);
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [filters]);

  useEffect(() => {
    if (!filters.brand) return;
    api.verdicts({ brand: filters.brand, limit: "50", sort: "median_desc" }).then(setDirectory).catch(() => {});
  }, [filters.brand]);

  // Dead-end rescue: ref selected but no verdict → raw comps (evidence, not a
  // band) + per-scope counts so the user can route to a scope that has data.
  useEffect(() => {
    if (verdict || !filters.ref) {
      setComps(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    api
      .comps(filters.ref)
      .then((d) => {
        if (!cancelled) setComps(d);
      })
      .catch(() => {
        if (!cancelled) setComps(null);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [verdict, filters.ref]);

  // Trend data: append-only verdict snapshots per cell.
  useEffect(() => {
    if (!verdict) {
      setHistory([]);
      return;
    }
    let cancelled = false;
    api
      .history({ cell_key: verdict.cell_key })
      .then((d) => {
        if (!cancelled) setHistory(d.history || []);
      })
      .catch(() => {
        if (!cancelled) setHistory([]);
      });
    return () => {
      cancelled = true;
    };
  }, [verdict]);

  // ⌘K ref paste: input looks like a reference number → resolve taxonomy live.
  useEffect(() => {
    const v = cmdInput.trim();
    const looksLikeRef = /^[a-z0-9][a-z0-9./-]{3,14}$/i.test(v) && /\d/.test(v);
    if (!openCmd || !looksLikeRef) {
      setRefLookup(null);
      return;
    }
    const t = setTimeout(() => {
      api
        .refLookup(v)
        .then(setRefLookup)
        .catch(() => setRefLookup(null));
    }, 250);
    return () => clearTimeout(t);
  }, [cmdInput, openCmd]);

  const hasVerdict = !!verdict;
  // Hero chain: curated ref art (when SKU selected) → dial-validated receipt → placeholder.
  const receiptHero = verdict?.hero_image_url || verdict?.receipts?.find((r) => r.image_url)?.image_url || "";

  return (
    <div className="min-h-screen bg-ink text-enamel selection:bg-brass/30">
      <header className="sticky top-0 z-40 ultraThinMaterial--ink border-b border-border">
        <div className="mx-auto max-w-[1280px] px-6 md:px-8 h-[56px] flex items-center justify-between gap-6">
          <div className="flex items-center gap-5">
            <motion.div whileTap={{ scale: 0.97 }} className="flex items-center gap-3">
              <div className="h-7 w-7 rounded-full border border-steel/35 flex items-center justify-center bg-enamel/5">
                <div className="h-3 w-3 rounded-full bg-brass shadow-[0_0_10px_rgba(193,154,91,0.6)]" />
              </div>
              <span className="font-display text-[17px] tracking-[-0.02em] text-enamel">WatchFairValue</span>
              <span className="hidden md:inline-flex items-center rounded-full bg-white/[0.06] border border-white/10 px-2.5 py-1 text-[10px] tracking-[0.16em] uppercase text-steel">Atelier Steel</span>
            </motion.div>
          </div>
        </div>
      </header>


      <main className="mx-auto max-w-[1280px] px-6 md:px-8 py-6 md:py-8 grid grid-cols-1 lg:grid-cols-[380px_1fr] gap-6">
        <section className="rounded-[18px] border border-border bg-[#14181D] overflow-hidden flex flex-col will-transform">

          <div className="p-6 space-y-7 flex-1">
            <VaultPicker />

            {filters.ref ? (
              <div>
                <label className="font-mono text-[10px] tracking-[0.16em] uppercase text-patina">Dial & material — derived from {filters.ref}</label>
                <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
                  <span className={clsx(chip({ active: true }), "cursor-default gap-1.5")}>
                    <span className="h-2.5 w-2.5 shrink-0 rounded-full border border-black/10" style={{ background: DIAL_SWATCHES[filters.dial] ?? UNKNOWN_SWATCH }} />
                    {filters.dial || "unknown"}
                  </span>
                  <span className={clsx(chip({ active: true }), "cursor-default")}>{MATERIAL_LABELS[filters.material] || filters.material || "unknown"}</span>
                  <span className="font-mono text-[10px] text-patina/70">read-only — the SKU decides, you don't configure a 126610LN green</span>
                </div>
              </div>
            ) : null}
          </div>
        </section>

        <section className="flex flex-col gap-6">
          <div className="rounded-[18px] border border-steel/20 bg-enamel text-ink overflow-hidden will-transform">
            <div className="px-6 md:px-8 pt-6 pb-5 border-b border-ink/10 flex items-center justify-between">
              <div className="flex items-center gap-3">
                <span className="inline-flex h-6 items-center rounded-full bg-ink px-2.5 text-[10px] font-mono tracking-[0.12em] uppercase text-enamel">Caliper — fair band</span>
                <span className="hidden md:inline font-mono text-[11px] text-ink/50">p25 — median — p75 • recency 900d • 12 receipts</span>
              </div>
              <span className={clsx("inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[10px] font-mono tracking-[0.12em] uppercase border", hasVerdict ? "bg-ink text-enamel border-ink" : "bg-white border-ink/10 text-ink/60")}>
                <span className={clsx("h-1.5 w-1.5 rounded-full", hasVerdict ? "bg-emerald-500" : "bg-signal")} />
                {hasVerdict ? `${verdict.count} comps` : "no verdict"}
              </span>
            </div>

            <div className="px-6 md:px-8 py-7 md:py-8">
              <AnimatePresence mode="popLayout">
                {loading ? (
                  <motion.div key="loading" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={springGentle} className="py-10 text-center">
                    <div className="mx-auto h-8 w-8 rounded-full border-2 border-ink/10 border-t-ink animate-spin" />
                    <div className="mt-3 font-mono text-[11px] tracking-[0.12em] uppercase text-ink/50">Measuring exact cell…</div>
                  </motion.div>
                ) : hasVerdict ? (
                  <motion.div key={verdict.cell_key} layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }} transition={springDefault}>
                    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                      <h1 className="font-display text-[28px] md:text-[34px] leading-none tracking-[-0.03em] text-ink">
                        {verdict.brand} <span className="italic font-normal">{verdict.model}</span>
                      </h1>
                      <span className="rounded-full bg-ink px-2.5 py-1 font-mono text-[10px] tracking-[0.12em] uppercase text-enamel">{verdict.dial || "unknown"} · {verdict.material || "unknown"} · {verdict.scope || "unknown"}</span>
                    </div>

                    <NumberFlowGroup>
                      <div className="mt-6 grid grid-cols-3 gap-4">
                        <div>
                          <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-ink/50">Fair low — p25</div>
                          <div className="mt-1 font-display text-[22px] tracking-[-0.02em] text-ink">
                            <NumberFlow value={verdict.fair_low} format={{ style: "currency", currency: "USD", maximumFractionDigits: 0 }} trend={-1} />
                          </div>
                        </div>
                        <div className="text-center">
                          <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-brass">Median — weighted</div>
                          <div className="mt-1 font-display text-[28px] tracking-[-0.03em] text-ink">
                            <NumberFlow value={verdict.median} format={{ style: "currency", currency: "USD", maximumFractionDigits: 0 }} trend={0} />
                          </div>
                        </div>
                        <div className="text-right">
                          <div className="font-mono text-[10px] tracking-[0.16em] uppercase text-ink/50">Fair high — p75</div>
                          <div className="mt-1 font-display text-[22px] tracking-[-0.02em] text-ink">
                            <NumberFlow value={verdict.fair_high} format={{ style: "currency", currency: "USD", maximumFractionDigits: 0 }} trend={1} />
                          </div>
                        </div>
                      </div>
                    </NumberFlowGroup>

                    <div className="mt-7">
                      <Caliper verdict={verdict} />
                      <div className="mt-3 flex items-center justify-between font-mono text-[10px] tracking-[0.08em] text-ink/50">
                        <span>p25 {fmtUSD(verdict.p25)}</span>
                        <span className="text-brass">median {fmtUSD(verdict.median)}</span>
                        <span>p75 {fmtUSD(verdict.p75)}</span>
                      </div>
                    </div>

                    {history.length >= 2 && (() => {
                      const meds = history.map((h) => h.median);
                      const lo = Math.min(...meds, verdict.p25) * 0.98;
                      const hi = Math.max(...meds, verdict.p75) * 1.02 || 1;
                      const W = 560, H = 56;
                      const x = (i: number) => (history.length === 1 ? 0 : (i / (history.length - 1)) * (W - 8) + 4);
                      const y = (m: number) => H - 6 - ((m - lo) / (hi - lo)) * (H - 12);
                      const first = history[0], last = history[history.length - 1];
                      const delta = first.median > 0 ? ((last.median - first.median) / first.median) * 100 : 0;
                      return (
                        <div className="mt-6 rounded-xl border border-ink/10 bg-white/50 p-4">
                          <div className="flex items-center justify-between font-mono text-[10px] tracking-[0.12em] uppercase text-ink/50">
                            <span>median trend — {history.length} nightly snapshots</span>
                            <span className={clsx(delta >= 0 ? "text-emerald-700" : "text-red-600")}>
                              {delta >= 0 ? "▲" : "▼"} {Math.abs(delta).toFixed(1)}% since {new Date(first.computed_at * 1000).toLocaleDateString("en-GB", { month: "short", year: "numeric" })}
                            </span>
                          </div>
                          <svg viewBox={`0 0 ${W} ${H}`} className="mt-2 w-full" preserveAspectRatio="none">
                            <polyline
                              points={history.map((h, i) => `${x(i)},${y(h.median)}`).join(" ")}
                              fill="none" stroke="#C19A5B" strokeWidth="2" strokeLinejoin="round" strokeLinecap="round"
                            />
                            {history.map((h, i) => (
                              <circle key={i} cx={x(i)} cy={y(h.median)} r="2.5" fill="#1A1E22" />
                            ))}
                          </svg>
                        </div>
                      );
                    })()}

                    <div className="mt-6 flex flex-wrap items-center gap-2 font-mono text-[11px]">
                      <span className="inline-flex items-center rounded-full bg-ink text-enamel px-2.5 py-1">{verdict.count} comps</span>
                      <span className="inline-flex items-center rounded-full border border-ink/15 bg-white px-2.5 py-1 text-ink/70">computed {new Date(verdict.computed_at).toLocaleDateString("en-GB")}</span>
                      <motion.button whileTap={{ scale: 0.97 }} onClick={() => { navigator.clipboard.writeText(verdict.cell_key); toast.success("Cell key copied"); if (navigator.vibrate) navigator.vibrate(10); }} className="ml-auto inline-flex items-center gap-1.5 rounded-full border border-ink/15 bg-white px-3 py-1 text-ink hover:border-ink/30 transition-colors button-press">
                        <span className="h-1.5 w-1.5 rounded-full bg-ink" /> {verdict.cell_key}
                      </motion.button>
                    </div>
                  </motion.div>
                ) : comps && comps.total > 0 ? (
                  <motion.div key="comps" initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: -8 }} transition={springDefault}>
                    <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                      <h1 className="font-display text-[24px] md:text-[30px] leading-none tracking-[-0.03em] text-ink">
                        {comps.brand} <span className="italic font-normal">{comps.model}</span>
                      </h1>
                      <span className="rounded-full bg-ink px-2.5 py-1 font-mono text-[10px] tracking-[0.12em] uppercase text-enamel">{comps.ref}</span>
                      <span className="rounded-full border border-ink/15 bg-white px-2.5 py-1 font-mono text-[10px] tracking-[0.12em] uppercase text-ink/70">{comps.dial || "unknown"} · {comps.material || "unknown"}</span>
                    </div>
                    <h3 className="mt-4 font-display text-[16px] tracking-[-0.02em] text-ink">{comps.total} identical sales — too few for a fair band, here they are</h3>
                    <p className="mt-1 font-mono text-[11px] leading-[1.5] text-ink/60">Evidence, not a verdict. Pick a scope with data — 4+ comps in one scope unlocks the exact-cell band.</p>
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {["", ...Object.keys(comps.scopes)].map((s) => {
                        const n = s === "" ? comps.total : comps.scopes[s] || 0;
                        return (
                          <motion.button
                            key={s || "unknown"}
                            whileTap={{ scale: 0.97 }}
                            onClick={() => { set({ scope: s }); if (navigator.vibrate) navigator.vibrate(10); }}
                            className={clsx("rounded-full border px-3 py-1.5 font-mono text-[11px] tracking-[0.08em] uppercase transition-colors", filters.scope === s ? "border-ink bg-ink text-enamel" : n > 0 ? "border-ink/20 bg-white text-ink hover:border-ink/40" : "border-ink/10 bg-white text-ink/35 cursor-default")}
                          >
                            {s === "" ? "any scope" : s.replace("_", " ")} · {n}
                          </motion.button>
                        );
                      })}
                    </div>
                    <div className="mt-5 divide-y divide-ink/10 rounded-xl border border-ink/10">
                      {comps.observations.slice(0, 8).map((c, i) => (
                        <a key={c.url + i} href={c.url} target="_blank" className="flex items-center gap-4 px-4 py-3 hover:bg-ink/[0.03] transition-colors">
                          <div className="min-w-0 flex-1">
                            <div className="truncate font-mono text-[12px] text-ink">{c.title}</div>
                            <div className="mt-0.5 font-mono text-[10px] text-ink/50">{c.source} · {c.date} · {c.scope || "unknown scope"}</div>
                          </div>
                          <div className="shrink-0 font-mono text-[13px] font-medium text-ink">{fmtUSD(c.price_usd)}</div>
                        </a>
                      ))}
                    </div>
                  </motion.div>
                ) : (
                  <motion.div key="empty" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={springGentle} className="py-10 text-center">
                    <div className="mx-auto inline-flex h-10 w-10 items-center justify-center rounded-full bg-signal/10 text-signal">✕</div>
                    <h3 className="mt-3 font-display text-[18px] tracking-[-0.02em] text-ink">No verdict — less than 4 identical sales</h3>
                    <p className="mx-auto mt-1.5 max-w-[520px] font-mono text-[12px] leading-[1.6] text-ink/60">{err || "Need at least 4 sales of that exact 5-tuple. Unknown matches unknown only — try clearing dial / material / scope."}</p>
                    <motion.button whileTap={{ scale: 0.97 }} onClick={resetDialMaterialScope} className="mt-4 inline-flex items-center rounded-full bg-ink px-4 py-2 font-mono text-[11px] tracking-[0.12em] uppercase text-enamel hover:bg-ink/90 transition-colors button-press">
                      Clear to unknown → retry
                    </motion.button>
                  </motion.div>
                )}
              </AnimatePresence>
            </div>
          </div>

          <AnimatePresence mode="popLayout">
            {hasVerdict && (
              <motion.div layout initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} exit={{ opacity: 0, y: 8 }} transition={springDefault} className="rounded-[18px] border border-border bg-[#14181D] overflow-hidden will-transform">
                <div className="px-6 py-4 border-b border-border flex items-center justify-between">
                  <h3 className="font-mono text-[11px] tracking-[0.14em] uppercase text-steel">Evidence — 12 most recent comps</h3>
                  <span className="font-mono text-[10px] text-patina">loupe on hover • drag caliper above to scrub</span>
                </div>
                <div className="divide-y divide-border">
                  {verdict.receipts.map((r, i) => (
                    <motion.a
                      key={r.url + i}
                      href={r.url}
                      target="_blank"
                      initial={{ opacity: 0 }}
                      animate={{ opacity: 1 }}
                      transition={{ ...springGentle, delay: i * 0.03 }}
                      whileHover={{ x: 2 }}
                      whileTap={{ scale: 0.99 }}
                      className="group flex items-center gap-4 px-6 py-3.5 hover:bg-white/[0.02] transition-colors will-transform"
                    >
                      <div className="hidden md:flex h-12 w-12 rounded-[10px] overflow-hidden border border-white/10 bg-ink shrink-0 group-hover:border-brass/30 transition-colors">
                        <img src={r.image_url || PLACEHOLDER} alt={r.title} className="h-full w-full object-cover" loading="lazy" onError={(e) => { (e.currentTarget as HTMLImageElement).src = PLACEHOLDER; }} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-mono text-[12px] leading-[1.4] text-enamel group-hover:text-white">{r.title}</div>
                        <div className="mt-1 flex flex-wrap items-center gap-2 font-mono text-[10px] tracking-[0.06em] text-patina">
                          <span className="inline-flex items-center rounded-full bg-white/[0.06] border border-white/10 px-2 py-0.5 text-steel group-hover:border-steel/30">{r.source}</span>
                          <span>{r.date}</span>
                          <span className="opacity-40">•</span>
                          <span className="truncate max-w-[180px] opacity-70">{r.ref || "no ref"}</span>
                        </div>
                      </div>
                      <div className="text-right shrink-0">
                        <div className="font-mono text-[13px] font-medium tracking-[-0.02em] text-enamel">{fmtUSD(r.price)}</div>
                        <div className="font-mono text-[10px] tracking-[0.08em] uppercase text-patina">USD</div>
                      </div>
                    </motion.a>
                  ))}
                </div>
              </motion.div>
            )}
          </AnimatePresence>
        </section>
      </main>

      <section className="mx-auto max-w-[1280px] px-6 md:px-8 pb-12">
        <div className="rounded-[18px] border border-border bg-[#14181D] overflow-hidden">
          <div className="px-6 py-4 border-b border-border flex flex-wrap items-center justify-between gap-3">
            <h3 className="font-display text-[13px] tracking-[0.14em] uppercase text-steel">Directory — {filters.brand} • {directory?.total ?? "—"} verdicts</h3>
            <span className="font-mono text-[10px] tracking-[0.12em] uppercase text-patina">Virtuoso • 273 cells • drag to scroll</span>
          </div>
          <div className="h-[380px]">
            {directory ? (
              <Virtuoso
                style={{ height: "100%" }}
                totalCount={directory.verdicts.length}
                itemContent={(index) => {
                  const v = directory.verdicts[index];
                  const isActive = v.cell_key === verdict?.cell_key;
                  return (
                    <Link
                      href={`/cell/${encodeURIComponent(v.cell_key)}`}
                      onClick={(e) => {
                        e.preventDefault();
                        const parts = v.cell_key.split("|");
                        set({ brand: parts[0], model: parts[1], ref: "", dial: parts[2], material: parts[3], scope: parts[4] });
                        window.scrollTo({ top: 0, behavior: "smooth" });
                        if (navigator.vibrate) navigator.vibrate(10);
                      }}
                      className={clsx(
                        "w-full text-left flex items-center gap-4 px-6 py-3 border-b border-border/60 hover:bg-white/[0.02] transition-colors will-transform",
                        isActive && "bg-brass/[0.08] border-l-2 border-l-brass"
                      )}
                    >
                      <motion.div layoutId={`active-${v.cell_key}`} className="h-10 w-10 rounded-[8px] overflow-hidden border border-white/10 bg-ink shrink-0">
                        <img src={(v as any).hero_image_url || PLACEHOLDER} alt={`${v.brand} ${v.model}`} className="h-full w-full object-cover" loading="lazy" onError={(e) => { (e.currentTarget as HTMLImageElement).src = PLACEHOLDER; }} />
                      </motion.div>
                      <div className="min-w-0 flex-1">
                        <div className="font-mono text-[12px] leading-none text-enamel truncate">
                          {v.brand} <span className="opacity-70">{v.model}</span> <span className="text-patina">· {v.dial || "unknown"} · {v.material || "unknown"} · {v.scope || "unknown"}</span>
                        </div>
                        <div className="mt-1 font-mono text-[10px] tracking-[0.08em] text-patina truncate">{v.cell_key}</div>
                      </div>
                      <div className="text-right shrink-0">
                        <div className="font-mono text-[12px] text-enamel">{fmtUSD(v.median)}</div>
                        <div className="font-mono text-[10px] text-patina">{v.count} comps</div>
                      </div>
                    </Link>
                  );
                }}
              />
            ) : (
              <div className="h-full flex items-center justify-center font-mono text-[11px] tracking-[0.12em] uppercase text-patina">Loading directory…</div>
            )}
          </div>
        </div>
        <div className="mt-3 flex justify-between font-mono text-[10px] tracking-[0.08em] text-patina">
          <span>Exact-cell only • unknown matches unknown</span>
          <a href="https://watchfetcher.fly.dev/api/meta/stats" target="_blank" className="hover:text-steel underline decoration-steel/30 underline-offset-4">
            api /meta/stats →
          </a>
        </div>
      </section>

      <footer className="border-t border-border mt-auto">
        <div className="mx-auto max-w-[1280px] px-6 md:px-8 h-[48px] flex items-center justify-between font-mono text-[10px] tracking-[0.12em] uppercase text-patina">
          <span>WatchFairValue — atelier steel • 900d half-life • dominant-cluster trim</span>
          <span className="hidden md:inline opacity-60">Built with base-ui • NumberFlow • motion • Virtuoso</span>
        </div>
      </footer>

      <AnimatePresence>
        {openCmd && (
          <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={springGentle}
            className="fixed inset-0 bg-ink/60 backdrop-blur-[6px] z-50 flex items-start justify-center pt-[20vh] p-4"
            onClick={() => setOpenCmd(false)}
          >
            <motion.div
              initial={{ y: 20, opacity: 0, scale: 0.98 }}
              animate={{ y: 0, opacity: 1, scale: 1 }}
              exit={{ y: 20, opacity: 0, scale: 0.98 }}
              transition={springDefault}
              drag="y"
              dragElastic={0.2}
              dragConstraints={{ top: 0, bottom: 300 }}
              onDragEnd={(_, info) => {
                if (info.offset.y > 80 || info.velocity.y > 500) setOpenCmd(false);
              }}
              onClick={(e) => e.stopPropagation()}
              className="w-full max-w-[560px] rounded-[18px] border border-border bg-[#1A1E22]/80 backdrop-blur-[20px] backdrop-saturate-150 overflow-hidden shadow-[0_20px_60px_rgba(0,0,0,0.5)] will-transform ultraThinMaterial"
            >
              <div className="flex justify-center pt-2">
                <div className="h-1 w-8 rounded-full bg-white/20" />
              </div>
              <Command>
                <Command.Input
                  placeholder="Search brand, model, cell_key — or paste a reference number"
                  onValueChange={setCmdInput}
                  className="w-full h-12 px-4 bg-transparent border-b border-border outline-none font-mono text-[13px] text-enamel placeholder:text-patina"
                />
                <Command.List className="max-h-[320px] overflow-auto p-2">
                  <Command.Empty className="px-3 py-8 text-center font-mono text-[12px] text-patina">No verdicts match.</Command.Empty>
                  {refLookup && (
                    <Command.Item
                      value={`reference ${refLookup.ref} ${refLookup.brand} ${refLookup.model}`}
                      onSelect={() => {
                        set({ brand: refLookup.brand, model: refLookup.model, ref: refLookup.ref, dial: refLookup.dial, material: refLookup.material, scope: "" });
                        setOpenCmd(false);
                        window.scrollTo({ top: 0, behavior: "smooth" });
                        if (navigator.vibrate) navigator.vibrate(10);
                      }}
                      className="flex items-center justify-between rounded-xl px-3 py-2.5 font-mono text-[12px] text-enamel aria-selected:bg-white/[0.06] cursor-pointer will-transform border border-brass/30 bg-brass/[0.06] mb-1"
                    >
                      <span className="truncate">
                        <span className="text-brass">ref →</span> {refLookup.ref} <span className="opacity-60">{refLookup.brand} {refLookup.model} · {refLookup.dial || "unknown"} · {refLookup.material || "unknown"}</span>
                      </span>
                      <span className="text-patina">{refLookup.count >= 4 ? fmtUSD(refLookup.median) : refLookup.count > 0 ? `${refLookup.count} comps` : "no comps"}</span>
                    </Command.Item>
                  )}
                  {directory?.verdicts.slice(0, 20).map((v: any) => (
                    <Command.Item
                      key={v.cell_key}
                      value={v.cell_key}
                      onSelect={() => {
                        const p = v.cell_key.split("|");
                        set({ brand: p[0], model: p[1], ref: "", dial: p[2], material: p[3], scope: p[4] });
                        setOpenCmd(false);
                        if (navigator.vibrate) navigator.vibrate(10);
                      }}
                      className="flex items-center justify-between rounded-xl px-3 py-2.5 font-mono text-[12px] text-enamel aria-selected:bg-white/[0.06] cursor-pointer will-transform"
                    >
                      <span className="truncate">
                        {v.brand} {v.model} <span className="opacity-60">· {v.cell_key}</span>
                      </span>
                      <span className="text-patina">{fmtUSD(v.median)}</span>
                    </Command.Item>
                  ))}
                </Command.List>
              </Command>
            </motion.div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

