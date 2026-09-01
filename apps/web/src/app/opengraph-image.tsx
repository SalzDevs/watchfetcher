import { ImageResponse } from "next/og";

export const size = { width: 1200, height: 630 };
export const contentType = "image/png";
export const alt = "WatchFairValue — what is this exact watch worth?";

export default async function Image() {
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
        <div style={{ display: "flex", alignItems: "center", gap: 16 }}>
          <div
            style={{
              width: 32,
              height: 32,
              borderRadius: 16,
              border: "1px solid rgba(200,209,219,0.35)",
              background: "rgba(237,233,225,0.05)",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            <div style={{ width: 12, height: 12, borderRadius: 6, background: "#C19A5B" }} />
          </div>
          <div style={{ fontFamily: "monospace", fontSize: 14, letterSpacing: "0.16em", textTransform: "uppercase", color: "#C8D1DB" }}>
            Atelier Steel
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column" }}>
          <div style={{ fontSize: 54, fontWeight: 400, letterSpacing: "-0.03em", lineHeight: 1 }}>WatchFairValue</div>
          <div style={{ marginTop: 12, fontSize: 20, color: "#C8D1DB", fontFamily: "monospace" }}>Every price is a measurement, not a guess.</div>
          <div style={{ marginTop: 16, display: "flex", gap: 12 }}>
            <div style={{ borderRadius: 999, background: "#EDE9E1", color: "#0F1418", padding: "8px 16px", fontFamily: "monospace", fontSize: 12, letterSpacing: "0.12em", textTransform: "uppercase" }}>
              273 verdicts • 5220 obs • exact-cell
            </div>
            <div style={{ borderRadius: 999, border: "1px solid rgba(200,209,219,0.22)", padding: "8px 16px", fontFamily: "monospace", fontSize: 11, color: "#C8D1DB" }}>
              4+ comps or no verdict
            </div>
          </div>
        </div>
        <div style={{ display: "flex", justifyContent: "space-between", fontFamily: "monospace", fontSize: 11, letterSpacing: "0.12em", textTransform: "uppercase", color: "#7A8A7F" }}>
          <span>watchfairvalue.fly.dev</span>
          <span>900d half-life • dominant-cluster trim</span>
        </div>
      </div>
    ),
    size
  );
}
