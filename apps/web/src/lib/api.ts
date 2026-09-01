const BASE =
  process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, "") ||
  "https://watchfetcher.fly.dev";

async function get<T>(path: string, params?: Record<string, string | undefined>): Promise<T> {
  const url = new URL(`${BASE}${path}`);
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      if (v != null && v !== "") url.searchParams.set(k, v);
    }
  }
  const res = await fetch(url.toString(), { next: { revalidate: 3600 } });
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    const err: any = new Error(body.message || body.error || res.statusText);
    err.status = res.status;
    err.code = body.error;
    throw err;
  }
  return res.json() as Promise<T>;
}

export type Receipt = {
  title: string;
  price: number;
  url: string;
  date: string;
  source: string;
  ref: string;
  image_url?: string;
};

export type Verdict = {
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
  receipts: Receipt[];
};

export type VerdictRow = Omit<Verdict, "receipts"> & { hero_image_url?: string };

export type Reference = {
  ref: string;
  brand: string;
  model: string;
  dial: string;
  material: string;
  image?: string;
  count: number;
  median: number;
  hero_image_url?: string;
};

export type RefComp = {
  title: string;
  price_usd: number;
  url: string;
  source: string;
  ref: string;
  scope: string;
  date: string;
  image_url?: string;
};

export type RefComps = {
  ref: string;
  brand: string;
  model: string;
  dial: string;
  material: string;
  total: number;
  scopes: Record<string, number>;
  observations: RefComp[];
};

export type RefLookup = {
  ref: string;
  brand: string;
  model: string;
  dial: string;
  material: string;
  image: string;
  count: number;
  median: number;
  cell_key?: string;
};

export type HistoryPoint = {
  computed_at: number;
  count: number;
  median: number;
  p25: number;
  p75: number;
};

export type BrandCollection = {
  brand: string;
  model_count: number;
  ref_count: number;
  total_comps: number;
  image?: string;
};

export type ModelCollection = {
  model: string;
  ref_count: number;
  total_comps: number;
  best_ref?: string;
  image?: string;
};

export const api = {
  health: () => get<{ status: string; verdicts: number; observations: number; computed_at: string; uptime_s: number }>("/health"),
  verdict: (params: { brand: string; model: string; dial?: string; material?: string; scope?: string; ref?: string } | { cell_key: string } | { ref: string; scope?: string }) =>
    get<Verdict>("/api/verdict", params as Record<string, string>),
  verdicts: (params: { brand?: string; model?: string; dial?: string; material?: string; scope?: string; limit?: string; offset?: string; sort?: string }) =>
    get<{ total: number; limit: number; offset: number; verdicts: VerdictRow[] }>("/api/verdicts", params),
  brands: () => get<string[]>("/api/meta/brands"),
  models: (brand?: string) => get<string[]>(brand ? `/api/meta/models?brand=${encodeURIComponent(brand)}` : "/api/meta/models" as any).catch(() => get<string[]>("/api/meta/models")),
  // overload to handle query
  modelsByBrand: (brand: string) => get<string[]>(`/api/meta/models`, { brand }),
  dials: () => get<string[]>("/api/meta/dials"),
  materials: () => get<string[]>("/api/meta/materials"),
  scopes: () => get<string[]>("/api/meta/scopes"),
  references: (brand: string, model: string) =>
    get<{ brand: string; model: string; references: Reference[] }>("/api/references", { brand, model }),
  refLookup: (ref: string) => get<RefLookup>("/api/reference", { ref }),
  collections: (brand?: string): Promise<{ collections?: BrandCollection[]; models?: ModelCollection[] }> =>
    brand
      ? get<{ brand: string; models: ModelCollection[] }>("/api/collections", { brand })
      : get<{ collections: BrandCollection[] }>("/api/collections"),
  comps: (ref: string) => get<RefComps>("/api/comps", { ref }),
  history: (params: { cell_key?: string; ref?: string }) =>
    get<{ cell_key: string; history: HistoryPoint[] }>("/api/history", params),
  stats: () => get<{ verdicts: number; observations: number; cells_with_data: number; computed_at: string }>("/api/meta/stats"),
};

// helpers for direct fetches with query
export async function fetchModels(brand?: string) {
  const url = brand ? `${BASE}/api/meta/models?brand=${encodeURIComponent(brand)}` : `${BASE}/api/meta/models`;
  const res = await fetch(url, { next: { revalidate: 86400 } });
  if (!res.ok) throw new Error("models fetch failed");
  return (await res.json()) as string[];
}
