import type { Metadata } from "next";
import { Instrument_Serif, Geist_Mono } from "next/font/google";
import { Toaster } from "sonner";
import "./globals.css";

const display = Instrument_Serif({
  variable: "--font-display",
  subsets: ["latin"],
  weight: ["400"],
  style: ["normal", "italic"],
});

const mono = Geist_Mono({
  variable: "--font-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  metadataBase: new URL("https://watchfairvalue.fly.dev"),
  title: {
    template: "%s | WatchFairValue",
    default: "WatchFairValue — what is this exact watch worth?",
  },
  description:
    "Exact-cell fair value for 273 pre-owned configurations. No sibling guessing, no phantom comps — 4 identical sales or no verdict.",
  alternates: {
    canonical: "/",
  },
  openGraph: {
    title: "WatchFairValue",
    description: "The atelier steel caliper for pre-owned watches.",
    type: "website",
    siteName: "WatchFairValue",
    locale: "en_US",
    url: "https://watchfairvalue.fly.dev",
    images: [{ url: "/opengraph-image", width: 1200, height: 630 }],
  },
  twitter: {
    card: "summary_large_image",
    title: "WatchFairValue — what is this exact watch worth?",
    description: "The atelier steel caliper for pre-owned watches.",
    images: ["/opengraph-image"],
  },
  robots: {
    index: true,
    follow: true,
  },
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${display.variable} ${mono.variable} h-full`}>
      <body className="min-h-full flex flex-col bg-ink text-enamel antialiased selection:bg-brass/30">
        {children}
        <Toaster
          position="bottom-right"
          theme="dark"
          toastOptions={{
            style: {
              background: "#1A1E22",
              border: "1px solid color-mix(in srgb, #C8D1DB 22%, #0F1418)",
              color: "#EDE9E1",
              fontFamily: "var(--font-mono)",
              fontSize: "12px",
              borderRadius: "12px",
            },
          }}
        />
      </body>
    </html>
  );
}
