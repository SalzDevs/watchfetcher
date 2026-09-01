import type { MetadataRoute } from "next";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const base = "https://watchfairvalue.fly.dev";
  const staticRoutes: MetadataRoute.Sitemap = [
    {
      url: base,
      lastModified: new Date(),
      changeFrequency: "daily",
      priority: 1,
    },
  ];

  try {
    const apiBase = process.env.NEXT_PUBLIC_API_URL || "https://watchfetcher.fly.dev";
    let offset = 0;
    const limit = 100;
    let total = Infinity;
    const all: { cell_key: string; computed_at: string; hero_image_url?: string }[] = [];

    while (all.length < total) {
      const res = await fetch(`${apiBase}/api/verdicts?limit=${limit}&offset=${offset}&sort=brand_asc`, {
        next: { revalidate: 3600 },
      });
      if (!res.ok) break;
      const data = (await res.json()) as { total: number; verdicts: { cell_key: string; computed_at: string; hero_image_url?: string }[] };
      total = data.total;
      all.push(...data.verdicts);
      offset += limit;
      if (data.verdicts.length === 0) break;
    }

    const dynamic = all.map((v) => ({
      url: `${base}/cell/${encodeURIComponent(v.cell_key)}`,
      lastModified: v.computed_at ? new Date(v.computed_at) : new Date(),
      changeFrequency: "weekly" as const,
      priority: 0.8 as const,
      images: v.hero_image_url ? [v.hero_image_url] : undefined,
    }));

    return [...staticRoutes, ...dynamic];
  } catch {
    return staticRoutes;
  }
}
