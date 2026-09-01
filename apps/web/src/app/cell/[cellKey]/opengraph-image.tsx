import { ImageResponse } from "next/og";

export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const alt = "WatchFairValue verdict";

export default async function Image({ params }: { params: Promise<{ cellKey: string }> }) {
  const { cellKey } = await params;
  const base = process.env.NEXT_PUBLIC_API_URL || "https://watchfetcher.fly.dev";
  let title = decodeURIComponent(cellKey);
  let median = "";
  let count = "";
  try {
    const res = await fetch(`${base}/api/verdict?cell_key=${encodeURIComponent(title)}`, { next: { revalidate: 3600 } });
    if (res.ok) {
      const v = (await res.json()) as { brand: string; model: string; median: number; count: number; dial: string };
      title = `${v.brand} ${v.model} ${v.dial}`.trim();
      median = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 }).format(v.median);
      count = `${v.count} comps`;
    }
  } catch {}

  return new ImageResponse(
    (
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          justifyContent: "space-between",
          width: "100%",
          height: "100%",
          background: "#0F1418",
          color: "#EDE9E1",
          padding: 48,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
          <div style={{ width: 28, height: 28, borderRadius: 14, border: "1px solid rgba(200,209,219,0.35)", display: "flex", alignItems: "center", justifyContent: "center" }}>
            <div style={{ width: 12, height: 12, borderRadius: 6, background: "#C19A5B" }} />
          </div>
          <div style={{ fontFamily: "monospace", fontSize: 12, letterSpacing: "0.16em", textTransform: "uppercase", color: "#C8D1DB" }}>WatchFairValue — Atelier Steel</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column" }}>
          <div style={{ fontSize: 42, fontWeight: 400, letterSpacing: "-0.03em", lineHeight: 1 }}>{title}</div>
          <div style={{ marginTop: 12, display: "flex", gap: 12, alignItems: "center" }}>
            <div style={{ background: "#EDE9E1", color: "#0F1418", borderRadius: 999, padding: "10px 18px", fontFamily: "monospace", fontSize: 22, fontWeight: 600 }}>{median || "—"}</div>
            <div style={{ borderRadius: 999, border: "1px solid rgba(200,209,219,0.22)", padding: "8px 14px", fontFamily: "monospace", fontSize: 12, color: "#C8D1DB" }}>{count}</div>
            <div style={{ fontFamily: "monospace", fontSize: 11, color: "#7A8A7F" }}>900d half-life • 12 receipts</div>
          </div>
        </div>
        <div style={{ display: "flex", justifyContent: "space-between", fontFamily: "monospace", fontSize: 11, letterSpacing: "0.12em", textTransform: "uppercase", color: "#7A8A7F" }}>
          <span>watchfairvalue.fly.dev</span>
          <span>4+ comps or no verdict</span>
        </div>
      </div>
    ),
    size
  );
}
