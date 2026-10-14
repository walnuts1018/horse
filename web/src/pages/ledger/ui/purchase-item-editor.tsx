import type { Household, ProductConcept, PurchaseItem } from "../../../shared/api";
import type { LedgerItemDraft } from "../model/purchase-item-draft";

import { EditPurchaseItemAllocations } from "../../../entities/purchase";
import { isOneOf } from "../../../shared/lib";
import { LedgerProductVariantSelect } from "./ledger-product-variant-select";

function allocationsInPoolOrder<T extends { poolId: string }>(
  allocations: T[],
  pools: Household["pools"],
) {
  const poolIDs = pools.map((pool) => pool.id);
  return [
    ...poolIDs.flatMap((poolId) =>
      allocations.filter((allocation) => allocation.poolId === poolId),
    ),
    ...allocations.filter((allocation) => !poolIDs.includes(allocation.poolId)),
  ];
}

export function PurchaseItemEditor({
  item,
  draft,
  household,
  merchant,
  categories,
  catalog,
  onChange,
}: {
  item: PurchaseItem;
  draft?: LedgerItemDraft;
  household: Household;
  merchant: string;
  categories: Array<{ id: string; name: string; archived: boolean }>;
  catalog: ProductConcept[];
  onChange: (updates: Partial<LedgerItemDraft>) => void;
}) {
  const defaultAllocations =
    household.defaultAllocation.length > 0
      ? household.defaultAllocation.map(({ poolId, weight }) => ({ poolId, weight }))
      : household.pools.map((pool) => ({
          poolId: pool.id,
          weight: pool.defaultShareWeight || 1,
        }));
  const allocations = allocationsInPoolOrder(
    draft?.allocations ?? item.allocations,
    household.pools,
  );
  const normalizedDefaultAllocations = allocationsInPoolOrder(defaultAllocations, household.pools);
  return (
    <>
      <input
        aria-label="品名"
        value={draft?.name ?? item.name}
        onChange={(event) => onChange({ name: event.target.value })}
      />
      <select
        aria-label="カテゴリ"
        value={draft?.categoryId ?? item.categoryId ?? ""}
        onChange={(event) => onChange({ categoryId: event.target.value })}
      >
        <option value="">未分類</option>
        {categories
          .filter((category) => !category.archived)
          .map((category) => (
            <option key={category.id} value={category.id}>
              {category.name}
            </option>
          ))}
      </select>
      <LedgerProductVariantSelect
        householdId={household.id}
        itemName={draft?.name ?? item.name}
        merchant={merchant}
        value={draft?.productVariantId ?? item.productVariantId ?? ""}
        catalog={catalog}
        onChange={(productVariantId, variantName) => {
          onChange({
            productVariantId,
            variantName,
          });
        }}
      />
      <EditPurchaseItemAllocations
        allocations={allocations}
        defaultAllocations={normalizedDefaultAllocations}
        pools={household.pools}
        onChange={(next) => onChange({ allocations: next })}
      />
      {draft?.productVariantId !== (item.productVariantId ?? "") && draft?.productVariantId && (
        <>
          <select
            aria-label="商品名の学習範囲"
            value={draft.productLearning}
            onChange={(event) =>
              onChange({
                productLearning: isOneOf(event.target.value, [
                  "ITEM_ONLY",
                  "MERCHANT_ALIAS",
                  "VARIANT_NAME",
                ])
                  ? event.target.value
                  : "ITEM_ONLY",
              })
            }
          >
            <option value="ITEM_ONLY">この品目だけ</option>
            <option value="VARIANT_NAME">バリエーション名として学習</option>
            <option value="MERCHANT_ALIAS">この店の別表記として学習</option>
          </select>
          {draft.productLearning === "VARIANT_NAME" && (
            <input
              aria-label="バリエーション名"
              value={draft.variantName}
              onChange={(event) => onChange({ variantName: event.target.value })}
              placeholder="バリエーション名"
            />
          )}
        </>
      )}
      <input
        aria-label="金額"
        inputMode="numeric"
        value={draft?.amountYen ?? String(item.amountYen)}
        onChange={(event) => onChange({ amountYen: event.target.value.replace(/\D/g, "") })}
      />
      <details className="item-measure-details">
        <summary>数量・印字単価</summary>
        <label>
          数量
          <input
            value={draft?.quantity ?? ""}
            onChange={(event) => onChange({ quantity: event.target.value })}
            placeholder="1、1/2"
          />
        </label>
        <label>
          単位
          <input
            value={draft?.quantityUnit ?? ""}
            onChange={(event) => onChange({ quantityUnit: event.target.value })}
            maxLength={32}
          />
        </label>
        <label>
          印字単価
          <input
            inputMode="numeric"
            value={draft?.printedUnitPriceYen ?? ""}
            onChange={(event) =>
              onChange({ printedUnitPriceYen: event.target.value.replace(/\D/g, "") })
            }
          />
        </label>
        <label>
          基準量
          <input
            value={draft?.printedBasisQuantity ?? ""}
            onChange={(event) => onChange({ printedBasisQuantity: event.target.value })}
            placeholder="1、100、1/2"
          />
        </label>
        <label>
          印字単位
          <input
            value={draft?.printedUnitPriceUnit ?? ""}
            onChange={(event) => onChange({ printedUnitPriceUnit: event.target.value })}
            maxLength={32}
          />
        </label>
      </details>
    </>
  );
}
