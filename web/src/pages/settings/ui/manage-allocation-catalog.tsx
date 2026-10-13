import styles from "./settings-page.module.css";
import { useEffect } from "react";
import { Check, Plus } from "lucide-react";
import type { Household } from "../../../shared/api";
import { useAllocationCatalog } from "../model/allocation-catalog";

export function ManageAllocationCatalog({
  household,
  allocationHidden,
  catalogHidden,
  onNotice,
  onFormClean,
  onPendingChange,
}: {
  household: Household;
  allocationHidden: boolean;
  catalogHidden: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormClean: (formID: string) => void;
  onPendingChange: (pending: boolean) => void;
}) {
  const model = useAllocationCatalog({ household, onNotice, onFormClean });
  useEffect(() => onPendingChange(model.pending), [model.pending, onPendingChange]);

  return (
    <>
      <section className={`panel ${styles.settingsSection}`} hidden={allocationHidden}>
        <div className={`panel-heading ${styles.panelHeading}`}>
          <div>
            <span className="eyebrow">自動負担</span>
            <h2>優先ルール</h2>
          </div>
          <span className="count-badge">{model.allocationRulesCount}</span>
        </div>
        <div className={styles.settingsRows}>
          {model.allocationRules.map((rule) => (
            <div className={styles.settingRow} key={rule.id}>
              <div className={`pool-avatar ${styles.poolAvatar}`}>
                {rule.target.__typename === "Category"
                  ? "分"
                  : rule.target.__typename === "MerchantAllocationRuleTarget"
                    ? "店"
                    : rule.target.__typename === "ProductConcept"
                      ? "品"
                      : "規"}
              </div>
              <div>
                <b>{rule.target.name}</b>
                <small>
                  {rule.allocations
                    .map(
                      (allocation) =>
                        `${household.pools.find((pool) => pool.id === allocation.poolId)?.name ?? "プール"} ${allocation.weight}`,
                    )
                    .join(" / ")}
                </small>
              </div>
            </div>
          ))}
        </div>
        {model.allocationRulesHasNextPage && (
          <button
            className="text-button"
            type="button"
            disabled={model.allocationRulesPageLoading}
            onClick={() => void model.loadMoreRules()}
          >
            {model.allocationRulesPageLoading ? "読み込み中" : "さらに表示"}
          </button>
        )}
        <form
          className="allocation-rule-form"
          data-unsaved-id="allocation-rule"
          onSubmit={model.saveRule}
        >
          <select
            value={model.ruleScope}
            onChange={(event) =>
              model.setRuleScope(
                event.target.value as "CATEGORY" | "MERCHANT" | "PRODUCT" | "PRODUCT_VARIANT",
              )
            }
          >
            <option value="CATEGORY">カテゴリ</option>
            <option value="MERCHANT">店名</option>
            <option value="PRODUCT">商品</option>
            <option value="PRODUCT_VARIANT">バリエーション</option>
          </select>
          {model.ruleScope === "CATEGORY" ? (
            <select
              value={model.ruleCategoryId}
              onChange={(event) => model.setRuleCategoryId(event.target.value)}
              required
            >
              <option value="">カテゴリを選択</option>
              {household.categories
                .filter((category) => !category.archived)
                .map((category) => (
                  <option key={category.id} value={category.id}>
                    {category.name}
                  </option>
                ))}
            </select>
          ) : model.ruleScope === "MERCHANT" ? (
            <input
              value={model.ruleMerchant}
              onChange={(event) => model.setRuleMerchant(event.target.value)}
              placeholder="店名"
              required
            />
          ) : model.ruleScope === "PRODUCT" ? (
            <select
              value={model.ruleProductId}
              onChange={(event) => model.setRuleProductId(event.target.value)}
              required
            >
              <option value="">商品を選択</option>
              {model.catalog
                .filter((product) => !product.archived)
                .map((product) => (
                  <option key={product.id} value={product.id}>
                    {product.name}
                  </option>
                ))}
            </select>
          ) : (
            <select
              value={model.ruleVariantId}
              onChange={(event) => model.setRuleVariantId(event.target.value)}
              required
            >
              <option value="">バリエーションを選択</option>
              {model.catalog.flatMap((product) =>
                product.variants
                  .filter((variant) => !variant.archived)
                  .map((variant) => (
                    <option key={variant.id} value={variant.id}>
                      {product.name} · {variant.name}
                    </option>
                  )),
              )}
            </select>
          )}
          <div className={styles.allocationSettings}>
            {household.pools.map((pool) => (
              <label key={pool.id}>
                <span>{pool.name}</span>
                <div className="number-field">
                  <input
                    inputMode="numeric"
                    value={model.ruleWeights[pool.id] ?? ""}
                    onChange={(event) =>
                      model.setRuleWeights((current) => ({
                        ...current,
                        [pool.id]: event.target.value.replace(/\D/g, ""),
                      }))
                    }
                  />
                  <small>比</small>
                </div>
              </label>
            ))}
          </div>
          <button className="button button-primary" disabled={model.pending}>
            <Check size={16} />
            ルールを保存
          </button>
        </form>
      </section>
      <section className={`panel ${styles.settingsSection}`} hidden={catalogHidden}>
        <div className={`panel-heading ${styles.panelHeading}`}>
          <div>
            <span className="eyebrow">商品カタログ</span>
            <h2>商品とバリエーション</h2>
          </div>
          <span className="count-badge">{model.catalog.length}</span>
        </div>
        <div className={styles.settingsRows}>
          {model.catalog.map((product) => (
            <div className={styles.settingRow} key={product.id}>
              <div className={`pool-avatar ${styles.poolAvatar}`}>品</div>
              <div>
                <b>{product.name}</b>
                <small>
                  {product.variants
                    .map((variant) =>
                      [variant.name, variant.brand, variant.packageSize]
                        .filter(Boolean)
                        .join(" · "),
                    )
                    .join(" / ") || "バリエーションなし"}
                </small>
                {product.variants.some((variant) => variant.aliases.length > 0) && (
                  <small>
                    {product.variants
                      .flatMap((variant) =>
                        variant.aliases.map((alias) => `${alias.merchantKey}: ${alias.alias}`),
                      )
                      .join(" / ")}
                  </small>
                )}
              </div>
            </div>
          ))}
        </div>
        <form
          className="inline-create"
          data-unsaved-id="create-concept"
          onSubmit={model.createConcept}
        >
          <input
            value={model.conceptName}
            onChange={(event) => model.setConceptName(event.target.value)}
            placeholder="商品名"
            required
          />
          <select
            value={model.conceptCategoryId}
            onChange={(event) => model.setConceptCategoryId(event.target.value)}
          >
            <option value="">カテゴリなし</option>
            {household.categories
              .filter((category) => !category.archived)
              .map((category) => (
                <option key={category.id} value={category.id}>
                  {category.name}
                </option>
              ))}
          </select>
          <button className="button button-soft" disabled={model.pending}>
            <Plus size={15} />
            追加
          </button>
        </form>
        <form
          className="allocation-rule-form"
          data-unsaved-id="create-variant"
          onSubmit={model.createVariant}
        >
          <select
            value={model.variantConceptId}
            onChange={(event) => model.setVariantConceptId(event.target.value)}
            required
          >
            <option value="">商品を選択</option>
            {model.catalog
              .filter((product) => !product.archived)
              .map((product) => (
                <option key={product.id} value={product.id}>
                  {product.name}
                </option>
              ))}
          </select>
          <input
            value={model.variantName}
            onChange={(event) => model.setVariantName(event.target.value)}
            placeholder="バリエーション名"
            required
          />
          <input
            value={model.variantBrand}
            onChange={(event) => model.setVariantBrand(event.target.value)}
            placeholder="ブランド"
          />
          <input
            value={model.variantPackageSize}
            onChange={(event) => model.setVariantPackageSize(event.target.value)}
            placeholder="内容量・規格"
          />
          <button className="button button-soft" disabled={model.pending}>
            <Plus size={15} />
            バリエーションを追加
          </button>
        </form>
        <form
          className="allocation-rule-form"
          data-unsaved-id="create-alias"
          onSubmit={model.createAlias}
        >
          <select
            value={model.aliasVariantId}
            onChange={(event) => model.setAliasVariantId(event.target.value)}
            required
          >
            <option value="">バリエーションを選択</option>
            {model.catalog.flatMap((product) =>
              product.variants
                .filter((variant) => !variant.archived)
                .map((variant) => (
                  <option key={variant.id} value={variant.id}>
                    {product.name} · {variant.name}
                  </option>
                )),
            )}
          </select>
          <input
            value={model.aliasMerchant}
            onChange={(event) => model.setAliasMerchant(event.target.value)}
            placeholder="店名"
            required
          />
          <input
            value={model.aliasName}
            onChange={(event) => model.setAliasName(event.target.value)}
            placeholder="店頭の表記"
            required
          />
          <button className="button button-soft" disabled={model.pending}>
            <Plus size={15} />
            別表記を追加
          </button>
        </form>
      </section>
    </>
  );
}
