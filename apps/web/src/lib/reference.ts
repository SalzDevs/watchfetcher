// Reference-picker helpers: dial swatches, material labels, curated image paths.

export const DIAL_SWATCHES: Record<string, string> = {
  batgirl: "#2a4a7f",
  batman: "#1e3a5f",
  black: "#0f1418",
  blue: "#1e40af",
  brown: "#92400e",
  champagne: "#f7e7c6",
  gold: "#d4af37",
  green: "#166534",
  grey: "#9ca3af",
  meteorite: "#78716c",
  panda: "#f5f1e8",
  pepsi: "#c0392b",
  salmon: "#ff8c61",
  silver: "#e5e7eb",
  tiffany: "#81d8d0",
  tropical: "#d6b38d",
  white: "#fdfcf8",
};

export const UNKNOWN_SWATCH = "#C8D1DB";

export const MATERIAL_LABELS: Record<string, string> = {
  ceramic: "ceramic",
  gold: "gold",
  platinum: "platinum",
  steel: "steel",
  titanium: "titanium",
  two_tone: "two-tone",
};

export const fmtUSD = (n: number) =>
  new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "USD",
    maximumFractionDigits: 0,
  }).format(n);

export function slugify(s: string): string {
  return s
    .toLowerCase()
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

// Curated reference art convention: /watches/{brand}/{model}/{REF}.webp.
// Missing files fall back to dial-validated receipt images (onError) —
// curated is an override, not a requirement.
export function curatedImagePath(brand: string, model: string, ref: string): string {
  return `/watches/${slugify(brand)}/${slugify(model)}/${ref.trim().toUpperCase()}.webp`;
}
