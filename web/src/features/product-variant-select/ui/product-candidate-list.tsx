import type { ReactNode } from "react";

import { Combobox } from "@base-ui/react/combobox";

export type ProductCandidateOptionData = {
  id: string;
  content: ReactNode;
  disabled?: boolean;
};

export function ProductCandidateList({
  candidates,
  label = "商品候補",
  empty,
}: {
  candidates: ProductCandidateOptionData[];
  label?: string;
  empty?: ReactNode;
}) {
  return (
    <Combobox.List className="product-variant-select__list" aria-label={label}>
      {candidates.map((candidate) => (
        <Combobox.Item key={candidate.id} value={candidate.id} disabled={candidate.disabled}>
          {candidate.content}
        </Combobox.Item>
      ))}
      <Combobox.Empty>{empty}</Combobox.Empty>
    </Combobox.List>
  );
}
