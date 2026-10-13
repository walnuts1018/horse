import type { ProductConcept, PurchaseItem } from "../../../shared/api";
import type { LedgerItemDraft } from "../model/purchase-item-draft";

export function PurchaseItemEditor({
  item,
  draft,
  categories,
  catalog,
  onChange,
  onSave,
}: {
  item: PurchaseItem;
  draft?: LedgerItemDraft;
  categories: Array<{ id: string; name: string; archived: boolean }>;
  catalog: ProductConcept[];
  onChange: (updates: Partial<LedgerItemDraft>) => void;
  onSave: () => void;
}) {
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
      <select
        aria-label="正規商品"
        value={draft?.productVariantId ?? item.productVariantId ?? ""}
        onChange={(event) => {
          const productVariantId = event.target.value;
          onChange({
            productVariantId,
            variantName:
              catalog
                .flatMap((concept) => concept.variants)
                .find((variant) => variant.id === productVariantId)?.name ?? "",
          });
        }}
      >
        <option value="">商品未選択</option>
        {catalog.flatMap((concept) =>
          concept.variants
            .filter((variant) => !variant.archived)
            .map((variant) => (
              <option key={variant.id} value={variant.id}>
                {concept.name} · {variant.name}
              </option>
            )),
        )}
      </select>
      {draft?.productVariantId !== (item.productVariantId ?? "") && draft?.productVariantId && (
        <>
          <select
            aria-label="商品名の学習範囲"
            value={draft.productLearning ?? "ITEM_ONLY"}
            onChange={(event) =>
              onChange({
                productLearning: event.target.value as LedgerItemDraft["productLearning"],
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
              value={draft.variantName ?? ""}
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
      <button className="text-button" type="button" onClick={onSave}>
        品目を保存
      </button>
    </>
  );
}
