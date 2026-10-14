import { Popover } from "@base-ui/react/popover";
import { ChevronDown } from "lucide-react";

import type { Household } from "../../../shared/api";
import type { PurchaseDraftItem } from "../../../shared/lib";

import { areAllocationsEquivalent } from "../model/allocation-equivalence";

import "./purchase-item-allocations.css";

const formatYen = (value: bigint) => `¥${new Intl.NumberFormat("ja-JP").format(value)}`;

export function EditPurchaseItemAllocations({
  allocations,
  defaultAllocations,
  defaultLabel = "標準",
  pools,
  amount,
  onChange,
}: {
  allocations: PurchaseDraftItem["allocations"];
  defaultAllocations: PurchaseDraftItem["allocations"];
  defaultLabel?: string;
  pools: Household["pools"];
  amount?: string;
  onChange: (allocations: PurchaseDraftItem["allocations"]) => void;
}) {
  const effectiveAllocations = allocations.length ? allocations : defaultAllocations;
  const amountYen = amount && /^\d+$/.test(amount) ? BigInt(amount) : null;
  const totalWeight = effectiveAllocations.reduce(
    (sum, allocation) => sum + BigInt(allocation.weight),
    0n,
  );
  const allocationAmounts = new Map<string, bigint>();
  if (amountYen !== null && totalWeight > 0n) {
    const remainders: Array<{ poolId: string; remainder: bigint }> = [];
    let allocated = 0n;
    for (const allocation of effectiveAllocations) {
      const weight = BigInt(allocation.weight);
      const numerator = amountYen * weight;
      const baseAmount = numerator / totalWeight;
      allocationAmounts.set(allocation.poolId, baseAmount);
      allocated += baseAmount;
      remainders.push({ poolId: allocation.poolId, remainder: numerator % totalWeight });
    }
    remainders.sort((a, b) => {
      if (a.remainder !== b.remainder) return a.remainder > b.remainder ? -1 : 1;
      if (a.poolId < b.poolId) return -1;
      if (a.poolId > b.poolId) return 1;
      return 0;
    });
    for (let index = 0; index < Number(amountYen - allocated); index++) {
      const poolId = remainders[index]?.poolId;
      if (!poolId) break;
      allocationAmounts.set(poolId, (allocationAmounts.get(poolId) ?? 0n) + 1n);
    }
  }
  const allocationName = () => {
    if (allocations.length === 0 || areAllocationsEquivalent(allocations, defaultAllocations))
      return defaultLabel;
    if (allocations.length === 1)
      return `${pools.find((pool) => pool.id === allocations[0].poolId)?.name ?? "口座"} 100%`;
    const labelWeight = allocations.reduce((sum, allocation) => sum + allocation.weight, 0);
    return allocations
      .map((allocation) => {
        const name = pools.find((pool) => pool.id === allocation.poolId)?.name ?? "口座";
        const percentage = Math.round((allocation.weight / labelWeight) * 100);
        return `${name} ${percentage}%`;
      })
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
                effectiveAllocations.find((allocation) => allocation.poolId === pool.id)?.weight ??
                0;
              const percentage =
                totalWeight > 0n
                  ? ((BigInt(weight) * 100n + totalWeight / 2n) / totalWeight).toString()
                  : "0";
              return (
                <label className="allocation-option" key={pool.id}>
                  <span className="allocation-pool">
                    <span>{pool.name}</span>
                    {amountYen !== null && (
                      <small>
                        {percentage}% · {formatYen(allocationAmounts.get(pool.id) ?? 0n)}
                      </small>
                    )}
                  </span>
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
            {allocations.length > 0 &&
              !areAllocationsEquivalent(allocations, defaultAllocations) && (
                <button type="button" className="text-button" onClick={() => onChange([])}>
                  標準に戻す
                </button>
              )}
          </Popover.Popup>
        </Popover.Positioner>
      </Popover.Portal>
    </Popover.Root>
  );
}
