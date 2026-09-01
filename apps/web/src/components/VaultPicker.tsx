"use client";

import { useEffect, useMemo, useState } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { clsx } from "clsx";
import { api, BrandCollection, ModelCollection, Reference } from "@/lib/api";
import { useFilters } from "@/lib/store";
import { slugify } from "@/lib/reference";
import { ReferenceCard } from "@/components/ReferenceCard";

const springStep = { type: "spring" as const, stiffness: 380, damping: 34, mass: 1 };
const springGentle = { type: "spring" as const, stiffness: 300, damping: 30, mass: 1 };

type Step = "brand" | "model" | "variant";

function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join("");
}

// BrandTile: logo + name only — no card chrome. Logo file is the official
// mark fetched by scripts/fetch-brand-logos.mjs; missing file falls back to a
// plain wordmark.
function BrandTile({ brand, active, onClick }: { brand: string; active: boolean; onClick: () => void }) {
  const [imgOk, setImgOk] = useState(true);
  return (
    <motion.button
      whileTap={{ scale: 0.96 }}
      whileHover={{ scale: 1.03 }}
      transition={springGentle}
      onClick={onClick}
      className={clsx(
        "flex h-[92px] flex-col items-center justify-center gap-2.5 rounded-xl will-transform transition-colors",
        active ? "bg-brass/[0.08]" : "hover:bg-white/[0.03]"
      )}
    >
      {imgOk ? (
        <img
          src={`/logos/${slugify(brand)}.png`}
          alt={brand}
          loading="lazy"
          onError={() => setImgOk(false)}
          className="h-10 w-auto max-w-[120px] object-contain"
        />
      ) : (
        <span className="font-display text-[15px] uppercase tracking-[0.1em] text-brass">{initials(brand)}</span>
      )}
      <span className={clsx("font-mono text-[9px] uppercase tracking-[0.18em]", active ? "text-brass" : "text-patina")}>{brand}</span>
    </motion.button>
  );
}

// ModelTile: receipt hero → curated ref art (API image) → local family image
// (scripts/fetch-family-images.mjs) → brass monogram. Ref-count badge kept —
// it is the "worth drilling" signal.
function ModelTile({ brand, model, active, onClick }: { brand: string; model: ModelCollection; active: boolean; onClick: () => void }) {
  // 0 = API image, 1 = local family image, 2 = monogram
  const [stage, setStage] = useState<0 | 1 | 2>(model.image ? 0 : 1);
  return (
    <motion.button
      whileTap={{ scale: 0.97 }}
      whileHover={{ scale: 1.02 }}
      transition={springGentle}
      onClick={onClick}
      className={clsx(
        "overflow-hidden rounded-xl border bg-ink text-left will-transform transition-colors",
        active ? "border-brass/60" : "border-border hover:border-steel/40"
      )}
    >
      <span className="relative block h-[64px] overflow-hidden bg-[#0F1418]">
        {/* blurred self-backdrop fills the crop; product stays whole + centered */}
        {stage !== 2 && (
          <img
            src={stage === 0 && model.image ? model.image : `/families/${slugify(brand)}/${slugify(model.model)}.png`}
            alt=""
            aria-hidden
            loading="lazy"
            className="absolute inset-0 h-full w-full scale-110 object-cover opacity-40 blur-[6px]"
          />
        )}
        {stage === 0 && model.image && (
          <img
            src={model.image}
            alt={model.model}
            loading="lazy"
            onError={() => setStage(1)}
            className="relative h-full w-full object-contain p-1"
          />
        )}
        {stage === 1 && (
          <img
            src={`/families/${slugify(brand)}/${slugify(model.model)}.png`}
            alt={model.model}
            loading="lazy"
            onError={() => setStage(2)}
            className="relative h-full w-full object-contain p-1"
          />
        )}
        {stage === 2 && (
          <span className="absolute inset-0 flex items-center justify-center bg-gradient-to-b from-white/[0.03] to-transparent font-mono text-[16px] tracking-[0.14em] text-brass/70">
            {initials(model.model)}
          </span>
        )}
        {model.ref_count > 0 && (
          <span className="absolute right-1.5 top-1.5 rounded-full bg-ink/85 px-1.5 py-0.5 font-mono text-[9px] text-brass border border-white/10">
            {model.ref_count} refs
          </span>
        )}
      </span>
      <span className="block px-2.5 py-2">
        <span className="block truncate font-mono text-[11px] leading-none text-enamel">{model.model}</span>
        <span className="mt-1 block font-mono text-[9px] text-patina">
          {model.total_comps > 0 ? `${model.total_comps} comps` : "no data yet"}
        </span>
      </span>
    </motion.button>
  );
}

// VaultPicker: image-first three-level drill — Brand (wordmark tiles) →
// Model (best-ref image tiles) → Variant (ReferenceCard grid). Replaces the
// brand/model selects + flat reference grid. Selection collapses to a strip;
// dial/material stay derived from the SKU, with free pickers as the vintage
// fallback (rendered by the page below this widget when ref === "").
export function VaultPicker() {
  const { filters, set } = useFilters();
  const derived: Step = !filters.brand ? "brand" : !filters.model ? "model" : "variant";
  const [step, setStep] = useState<Step>(derived);
  const [open, setOpen] = useState(true);
  const [query, setQuery] = useState("");

  const [brands, setBrands] = useState<BrandCollection[] | null>(null);
  const [models, setModels] = useState<ModelCollection[] | null>(null);
  const [refs, setRefs] = useState<Reference[] | null>(null);

  // External selection (⌘K, directory, URL) syncs the drill position.
  useEffect(() => {
    setStep(derived);
    setQuery("");
  }, [filters.brand, filters.model, filters.ref]);

  useEffect(() => {
    if (step !== "brand" || brands) return;
    api.collections().then((d) => setBrands(d.collections ?? [])).catch(() => setBrands([]));
  }, [step, brands]);

  useEffect(() => {
    if (step === "brand" || !filters.brand) return;
    setModels(null);
    let cancelled = false;
    api
      .collections(filters.brand)
      .then((d) => {
        if (!cancelled) setModels(d.models ?? []);
      })
      .catch(() => {
        if (!cancelled) setModels([]);
      });
    return () => {
      cancelled = true;
    };
  }, [step, filters.brand]);

  useEffect(() => {
    if (step !== "variant" || !filters.brand || !filters.model) return;
    setRefs(null);
    let cancelled = false;
    api
      .references(filters.brand, filters.model)
      .then((d) => {
        if (!cancelled) setRefs(d.references);
      })
      .catch(() => {
        if (!cancelled) setRefs([]);
      });
    return () => {
      cancelled = true;
    };
  }, [step, filters.brand, filters.model]);

  const q = query.trim().toLowerCase();
  const brandList = useMemo(
    () => (brands || []).filter((b) => !q || b.brand.toLowerCase().includes(q)),
    [brands, q]
  );
  const modelList = useMemo(
    () => (models || []).filter((m) => !q || m.model.toLowerCase().includes(q)),
    [models, q]
  );
  const refList = useMemo(
    () => (refs || []).filter((r) => !q || r.ref.toLowerCase().includes(q) || r.dial.includes(q) || r.material.includes(q)),
    [refs, q]
  );

  const pickBrand = (b: string) => {
    set({ brand: b, model: "", ref: "", dial: "", material: "" });
    setStep("model");
    setQuery("");
  };
  const pickModel = (m: string) => {
    set({ model: m, ref: "", dial: "", material: "" });
    setStep("variant");
    setQuery("");
  };
  const pickRef = (r: Reference) => {
    set({ ref: r.ref, dial: r.dial, material: r.material });
    if (navigator.vibrate) navigator.vibrate(10);
    setOpen(false); // collapse to strip — receipts get the room
  };

  return (
    <div>
      <AnimatePresence mode="wait" initial={false}>
        {!open ? (
          // Collapsed selection strip — identity + entry back into the drill.
          <motion.button
            key="strip"
            initial={{ opacity: 0, y: -6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={springGentle}
            onClick={() => {
              setOpen(true);
              setStep("variant");
            }}
            className="mt-2.5 flex w-full items-center gap-3 rounded-xl border border-brass/40 bg-brass/[0.06] p-2.5 text-left will-transform hover:border-brass/60 transition-colors"
          >
            <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-white/10 bg-ink font-mono text-[10px] text-brass/80">
              {initials(filters.model)}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate font-mono text-[12px] leading-none text-enamel">
                {filters.brand} · {filters.model}
              </span>
              <span className="mt-1 block truncate font-mono text-[10px] text-patina">
                {filters.ref} · {filters.dial || "unknown"} · {filters.material || "unknown"}
              </span>
            </span>
            <span className="shrink-0 font-mono text-[10px] tracking-[0.1em] uppercase text-brass">change ▸</span>
          </motion.button>
        ) : (
          <motion.div
            key="drill"
            initial={{ opacity: 0, y: -6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={springGentle}
            className="mt-2.5"
          >
            {/* Breadcrumb — only once a maison is picked; each crumb jumps back */}
            {step !== "brand" && (
            <div className="flex items-center gap-1.5 pb-2.5">
              <motion.button
                whileTap={{ scale: 0.94 }}
                onClick={() => {
                  if (step === "variant") setStep("model");
                  else set({ brand: "", model: "", ref: "", dial: "", material: "" });
                }}
                className="inline-flex h-6 w-6 items-center justify-center rounded-full border border-border bg-ink text-[11px] text-steel hover:border-steel/40 hover:text-enamel transition-colors"
                aria-label="back"
              >
                ‹
              </motion.button>
              <button
                onClick={() => set({ brand: "", model: "", ref: "", dial: "", material: "" })}
                className="font-mono text-[11px] tracking-[0.06em] text-patina hover:text-steel"
              >
                {filters.brand}
              </button>
              {filters.brand && (
                <>
                  <span className="text-patina/40">›</span>
                  <button
                    onClick={() => {
                      setStep("model");
                      set({ model: "", ref: "", dial: "", material: "" });
                    }}
                    className={clsx("font-mono text-[11px] tracking-[0.06em]", step === "model" ? "text-enamel" : "text-patina hover:text-steel")}
                  >
                    {filters.model || "family"}
                  </button>
                </>
              )}
              {step === "variant" && filters.model && (
                <>
                  <span className="text-patina/40">›</span>
                  <span className="font-mono text-[11px] tracking-[0.06em] text-enamel">{filters.ref || "reference"}</span>
                </>
              )}
            </div>
            )}

            <AnimatePresence mode="wait" initial={false}>
              {step === "brand" && (
                <motion.div key="brand" initial={{ x: 28, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: -28, opacity: 0 }} transition={springStep} className="will-transform">
                  {brands === null ? (
                    <div className="flex h-[220px] items-center justify-center font-mono text-[10px] tracking-[0.14em] uppercase text-patina">Opening the vault…</div>
                  ) : brandList.length === 0 ? (
                    <div className="flex h-[120px] items-center justify-center font-mono text-[11px] text-patina">No maison matches “{query}”.</div>
                  ) : (
                    <div className="grid max-h-[356px] grid-cols-2 gap-x-2 gap-y-3 overflow-auto pr-1">
                      {brandList.map((c) => (
                        <BrandTile key={c.brand} brand={c.brand} active={filters.brand === c.brand} onClick={() => pickBrand(c.brand)} />
                      ))}
                    </div>
                  )}
                </motion.div>
              )}

              {step === "model" && (
                <motion.div key="model" initial={{ x: 28, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: -28, opacity: 0 }} transition={springStep} className="will-transform">
                  {models === null ? (
                    <div className="flex h-[220px] items-center justify-center font-mono text-[10px] tracking-[0.14em] uppercase text-patina">Loading families…</div>
                  ) : modelList.length === 0 ? (
                    <div className="flex h-[120px] items-center justify-center font-mono text-[11px] text-patina">No family matches “{query}”.</div>
                  ) : (
                    <div className="grid max-h-[356px] grid-cols-2 gap-1.5 overflow-auto pr-1">
                      {modelList.map((m) => (
                        <ModelTile key={m.model} brand={filters.brand} model={m} active={filters.model === m.model} onClick={() => pickModel(m.model)} />
                      ))}
                    </div>
                  )}
                </motion.div>
              )}

              {step === "variant" && (
                <motion.div key="variant" initial={{ x: 28, opacity: 0 }} animate={{ x: 0, opacity: 1 }} exit={{ x: -28, opacity: 0 }} transition={springStep} className="will-transform">
                  {refs === null ? (
                    <div className="flex h-[160px] items-center justify-center font-mono text-[10px] tracking-[0.14em] uppercase text-patina">Loading references…</div>
                  ) : (
                    <>
                      <div className="grid max-h-[300px] grid-cols-1 gap-1.5 overflow-auto pr-1">
                        {refList.map((r) => (
                          <ReferenceCard
                            key={r.ref}
                            reference={r}
                            image={r.image || undefined}
                            active={filters.ref === r.ref}
                            onSelect={() => pickRef(r)}
                          />
                        ))}
                        {refList.length === 0 && (
                          <div className="flex h-[80px] items-center justify-center font-mono text-[11px] text-patina">No reference matches “{query}”.</div>
                        )}
                      </div>
                      <motion.button
                        whileTap={{ scale: 0.97 }}
                        onClick={() => {
                          set({ ref: "", dial: "", material: "" });
                          if (navigator.vibrate) navigator.vibrate(10);
                        }}
                        className={clsx(
                          "mt-1.5 w-full rounded-xl border border-dashed p-2.5 text-left font-mono text-[11px] will-transform transition-colors",
                          filters.ref === "" ? "border-brass/60 text-brass" : "border-border text-patina hover:border-steel/40 hover:text-steel"
                        )}
                      >
                        {filters.ref === "" ? "✓ Other / vintage — picking dial freely" : "Other / vintage — no catalogued SKU, pick dial freely"}
                      </motion.button>
                    </>
                  )}
                </motion.div>
              )}
            </AnimatePresence>
          </motion.div>
        )}
      </AnimatePresence>

    </div>
  );
}
