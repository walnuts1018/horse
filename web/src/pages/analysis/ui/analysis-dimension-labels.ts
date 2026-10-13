import type { Dimension } from "../model/purchase-analytics";

export const analysisDimensionLabels: Record<Dimension, string> = {
  category: "カテゴリ",
  merchant: "店舗",
  item: "品目",
  pool: "負担先",
  account: "決済元",
  member: "メンバー負担",
};
