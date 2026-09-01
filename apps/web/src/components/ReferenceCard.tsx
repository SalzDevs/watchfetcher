"use client";

import { useState } from "react";
import { motion } from "framer-motion";
import { clsx } from "clsx";
import type { Reference } from "@/lib/api";
import { DIAL_SWATCHES, MATERIAL_LABELS, UNKNOWN_SWATCH, fmtUSD } from "@/lib/reference";

const springGentle = { type: "spring" as const, stiffness: 300, damping: 30, mass: 1 };

function initials(name: string) {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((w) => w[0]!.toUpperCase())
    .join("");
}

// ReferenceCard is one factory SKU in the vault picker: real product image
// (dial disc overlapping the corner) with brass-monogram fallback, derived
// dial/material — read-only, never picked. count < 4 comps → no verdict
// possible → greyed but still selectable (inspect the cell, no band).
export function ReferenceCard({
  reference,
  image,
  active,
  onSelect,
}: {
  reference: Reference;
  image?: string;
  active: boolean;
  onSelect: () => void;
}) {
  const [imgOk, setImgOk] = useState(true);
  const swatch = DIAL_SWATCHES[reference.dial] ?? UNKNOWN_SWATCH;
  const hasVerdict = reference.count >= 4;
  return (
    <motion.button
      whileTap={{ scale: 0.97 }}
      whileHover={{ scale: 1.01 }}
      transition={springGentle}
      onClick={onSelect}
      aria-pressed={active}
      className={clsx(
        "flex w-full items-center gap-3 rounded-xl border p-2.5 text-left will-transform transition-[border,background] duration-[100ms]",
        active
          ? "border-brass bg-brass/[0.08] shadow-[0_2px_10px_rgba(193,154,91,0.25)]"
          : "border-border bg-ink hover:border-steel/40"
      )}
    >
      <span className="relative h-12 w-12 shrink-0 overflow-hidden rounded-[10px] border border-white/10 bg-ink">
        {image && imgOk && (
          <>
            <img src={image} alt="" aria-hidden loading="lazy" className="absolute inset-0 h-full w-full scale-110 object-cover opacity-40 blur-[4px]" />
            <img src={image} alt={reference.ref} loading="lazy" onError={() => setImgOk(false)} className="relative h-full w-full object-contain p-0.5" />
          </>
        )}
        {(!image || !imgOk) && (
          <span className="flex h-full w-full items-center justify-center font-mono text-[11px] tracking-[0.06em] text-brass/80">
            {initials(reference.model)}
          </span>
        )}
        <span
          className="absolute -bottom-[3px] -right-[3px] h-3.5 w-3.5 rounded-full border border-ink shadow-[0_1px_4px_rgba(0,0,0,0.5)]"
          style={{ background: swatch }}
          title={`${reference.dial || "unknown"} dial`}
        />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block font-mono text-[12px] leading-none text-enamel">{reference.ref}</span>
        <span className="mt-1 block font-mono text-[10px] tracking-[0.06em] text-patina">
          {reference.dial || "unknown"} · {MATERIAL_LABELS[reference.material] || reference.material || "unknown"}
        </span>
      </span>
      <span className="shrink-0 text-right">
        {hasVerdict ? (
          <>
            <span className="block font-mono text-[12px] leading-none text-enamel">{fmtUSD(reference.median)}</span>
            <span className="mt-1 block font-mono text-[10px] text-patina">{reference.count} comps</span>
          </>
        ) : (
          <span className="block font-mono text-[10px] leading-[1.4] text-patina/70">
            {reference.count > 0 ? `${reference.count} comps` : "no comps"}
            <br />
            &lt;4 → no verdict
          </span>
        )}
      </span>
    </motion.button>
  );
}
