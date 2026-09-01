import { create } from "zustand";

export type Filters = {
  brand: string;
  model: string;
  ref: string;
  dial: string;
  material: string;
  scope: string;
};

type State = {
  filters: Filters;
  set: (patch: Partial<Filters>) => void;
  resetDialMaterialScope: () => void;
  cellKey: () => string;
};

const initial: Filters = {
  brand: "Rolex",
  model: "",
  ref: "",
  dial: "",
  material: "",
  scope: "",
};

export const useFilters = create<State>((set, get) => ({
  filters: initial,
  set: (patch) => set((s) => ({ filters: { ...s.filters, ...patch } })),
  resetDialMaterialScope: () => set((s) => ({ filters: { ...s.filters, ref: "", dial: "", material: "", scope: "" } })),
  cellKey: () => {
    const f = get().filters;
    return [f.brand, f.model, f.dial, f.material, f.scope].map((s) => s.trim().toLowerCase()).join("|");
  },
}));
