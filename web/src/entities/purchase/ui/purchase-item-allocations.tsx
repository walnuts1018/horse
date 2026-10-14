import { Popover } from "@base-ui/react/popover";
import { ChevronDown } from "lucide-react";

import type { Household } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";

import "./purchase-item-allocations.css";

export function EditPurchaseItemAllocations({
  allocations,
  defaultAllocations,
  pools,
  onChange,
}: {
  allocations: PurchaseDraftItem["allocations"];
  defaultAllocations: PurchaseDraftItem["allocations"];
  pools: Household["pools"];
  onChange: (allocations: PurchaseDraftItem["allocations"]) => void;
}) {
  const allocationName = () => {
    if (
      allocations.length === 0 ||
      JSON.stringify(allocations) === JSON.stringify(defaultAllocations)
    )
      return "標準";
    if (allocations.length === 1)
      return `${pools.find((pool) => pool.id === allocations[0].poolId)?.name ?? "口座"} 100%`;
    return allocations
      .map((allocation) => pools.find((pool) => pool.id === allocation.poolId)?.name ?? "口座")
      .join(" / ");
  };

  return (
    <Popover.Root>
      <Popover.Trigger className="allocation-chip">
        <span className="allocation-label">{allocationName()}</span>
        <ChevronDown size={12} />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Positioner sideOffset={6}>
          <Popover.Popup className="allocation-popup">
            <Popover.Title>負担割合</Popover.Title>
            {pools.map((pool) => (
              <button
                type="button"
                className="text-button"
                key={pool.id}
                onClick={() => onChange([{ poolId: pool.id, weight: 1 }])}
              >
                {pool.name} 100%
              </button>
            ))}
            <button
              type="button"
              className="text-button"
              disabled={pools.length === 0}
              onClick={() => onChange(pools.map((pool) => ({ poolId: pool.id, weight: 1 })))}
            >
              均等
            </button>
            {pools.map((pool) => {
              const weight =
                (allocations.length ? allocations : defaultAllocations).find(
                  (allocation) => allocation.poolId === pool.id,
                )?.weight ?? 0;
              return (
                <label className="allocation-option" key={pool.id}>
                  <span>{pool.name}</span>
                  <span className="number-field">
                    <input
                      aria-label={`${pool.name}の負担比率`}
                      inputMode="numeric"
                      value={weight || ""}
                      onChange={(event) => {
                        const nextWeight = Math.max(
                          0,
                          Number(event.target.value.replace(/\D/g, "")) || 0,
                        );
                        const base = allocations.length ? allocations : defaultAllocations;
                        const nextAllocations = base.filter(
                          (allocation) => allocation.poolId !== pool.id,
                        );
                        if (nextWeight > 0)
                          nextAllocations.push({ poolId: pool.id, weight: nextWeight });
                        onChange(nextAllocations);
                      }}
                    />
                    <small>比</small>
                  </span>
                </label>
              );
            })}
            <button type="button" className="text-button" onClick={() => onChange([])}>
              標準に戻す
            </button>
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
