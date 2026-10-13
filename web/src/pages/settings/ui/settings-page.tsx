import styles from "./settings-page.module.css";
import { useEffect, useState, type ChangeEvent, type FormEvent } from "react";
import { Activity, Check, Plus } from "lucide-react";
import { readGraphql, type Household, type AccountBalance } from "../../../shared/api";
import {
  createCategoryMutation,
  setCategoryArchivedMutation,
  setDefaultAllocationMutation,
} from "../api/operations";
import { ManageHouseholdInvitations } from "./manage-household-invitations";
import { ManageBudgets } from "./manage-budgets";
import { ManageRecurringExpenses } from "./manage-recurring-expenses";
import { ManageTags } from "./manage-tags";
import { ViewAccountBalanceHistory } from "./view-account-balance-history";
import { ManageAccountActivity } from "./manage-account-activity";
import { ManageAllocationCatalog } from "./manage-allocation-catalog";
import { ManageAccountSettings } from "./manage-account-settings";
import { accountBalancesQuery } from "../../../entities/ledger";

type SettingsTab = "accounts" | "allocation" | "catalog" | "planning" | "members";

const settingsTabs: { id: SettingsTab; label: string }[] = [
  { id: "accounts", label: "口座" },
  { id: "allocation", label: "負担" },
  { id: "catalog", label: "分類" },
  { id: "planning", label: "予算" },
  { id: "members", label: "メンバー" },
];

const settingsTabByHash: Record<string, SettingsTab> = {
  "pool-settings": "accounts",
  "payment-source-settings": "accounts",
  "default-allocation-settings": "allocation",
};

export function SettingsPage({
  household,
  onRefresh,
  onNotice,
  onUnsavedChanges,
  isOwner,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onUnsavedChanges: (unsaved: boolean) => void;
  isOwner: boolean;
}) {
  const [activeSettingsTab, setActiveSettingsTab] = useState<SettingsTab>("accounts");
  const [setupHashTarget, setSetupHashTarget] = useState<string | null>(null);
  const [categoryName, setCategoryName] = useState("");
  const [shareWeights, setShareWeights] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
    ),
  );
  const [pending, setPending] = useState(false);
  const [allocationCatalogPending, setAllocationCatalogPending] = useState(false);
  const [accountSettingsPending, setAccountSettingsPending] = useState(false);
  const [accountActivityPending, setAccountActivityPending] = useState(false);
  const [tagPending, setTagPending] = useState(false);
  const [invitationPending, setInvitationPending] = useState(false);
  const [dirtyForms, setDirtyForms] = useState<Set<string>>(() => new Set());
  const [budgetPending, setBudgetPending] = useState(false);
  const [recurringPending, setRecurringPending] = useState(false);
  const [balances, setBalances] = useState<AccountBalance[]>([]);
  const [balanceHistoryRevision, setBalanceHistoryRevision] = useState(0);
  useEffect(() => {
    onUnsavedChanges(
      pending ||
        accountActivityPending ||
        allocationCatalogPending ||
        accountSettingsPending ||
        tagPending ||
        budgetPending ||
        recurringPending ||
        invitationPending ||
        dirtyForms.size > 0,
    );
  }, [
    accountActivityPending,
    allocationCatalogPending,
    accountSettingsPending,
    budgetPending,
    dirtyForms,
    invitationPending,
    onUnsavedChanges,
    pending,
    recurringPending,
    tagPending,
  ]);
  useEffect(() => () => onUnsavedChanges(false), [onUnsavedChanges]);
  useEffect(() => {
    const selectSettingsTabFromHash = () => {
      const sectionId = window.location.hash.slice(1);
      const tab = settingsTabByHash[sectionId];
      if (!tab) {
        setSetupHashTarget(null);
        return;
      }
      setActiveSettingsTab(tab);
      setSetupHashTarget(sectionId);
    };
    selectSettingsTabFromHash();
    window.addEventListener("hashchange", selectSettingsTabFromHash);
    return () => window.removeEventListener("hashchange", selectSettingsTabFromHash);
  }, []);
  useEffect(() => {
    if (!setupHashTarget || settingsTabByHash[setupHashTarget] !== activeSettingsTab) return;
    document.getElementById(setupHashTarget)?.scrollIntoView({ block: "start" });
    setSetupHashTarget(null);
  }, [activeSettingsTab, setupHashTarget]);
  const markFormDirty = (formID: string) => {
    setDirtyForms((current) => {
      if (current.has(formID)) return current;
      const next = new Set(current);
      next.add(formID);
      return next;
    });
  };
  const markFormClean = (formID: string) => {
    setDirtyForms((current) => {
      if (!current.has(formID)) return current;
      const next = new Set(current);
      next.delete(formID);
      return next;
    });
  };
  const trackFormChange = (event: ChangeEvent<HTMLDivElement>) => {
    if (!(event.target instanceof Element)) return;
    const form = event.target.closest<HTMLFormElement>("form[data-unsaved-id]");
    if (form?.dataset.unsavedId) markFormDirty(form.dataset.unsavedId);
  };
  const refreshPlanning = async () => {
    const result = await readGraphql<{ accountBalances: AccountBalance[] }>(accountBalancesQuery, {
      householdId: household.id,
    });
    setBalances(result.accountBalances);
  };
  useEffect(() => {
    void refreshPlanning().catch((reason) => onNotice((reason as Error).message, "error"));
  }, [household.id, isOwner]);
  const saveMutation = async (
    mutation: Parameters<typeof readGraphql>[0],
    input: Record<string, unknown>,
    formID: string,
  ): Promise<boolean> => {
    setPending(true);
    try {
      await readGraphql<Record<string, unknown>>(mutation, { input });
      await onRefresh();
      markFormClean(formID);
      onNotice("設定を保存しました", "success");
      return true;
    } catch (reason) {
      onNotice((reason as Error).message, "error");
      return false;
    } finally {
      setPending(false);
    }
  };
  const addCategory = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(
      createCategoryMutation,
      {
        householdId: household.id,
        name: categoryName.trim(),
      },
      "create-category",
    ).then((saved) => saved && setCategoryName(""));
  };
  const saveAllocation = (event: FormEvent) => {
    event.preventDefault();
    void saveMutation(
      setDefaultAllocationMutation,
      {
        householdId: household.id,
        allocations: household.pools
          .map((pool) => ({
            poolId: pool.id,
            weight: Math.max(0, Number(shareWeights[pool.id]) || 0),
          }))
          .filter((item) => item.weight > 0),
      },
      "default-allocation",
    );
  };
  const archiveCategory = async (categoryId: string, archived: boolean) => {
    setPending(true);
    try {
      await readGraphql(setCategoryArchivedMutation, { input: { categoryId, archived } });
      await onRefresh();
      onNotice(archived ? "カテゴリを非表示にしました" : "カテゴリを再表示しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };
  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">共有と分類</span>
          <h1>設定</h1>
          <p>{household.name}の口座、決済元、カテゴリ</p>
        </div>
      </div>
      <nav
        aria-label="設定項目"
        style={{ display: "flex", flexWrap: "wrap", gap: 8, marginBottom: 14 }}
      >
        {settingsTabs.map((tab) => (
          <button
            key={tab.id}
            type="button"
            className={`button ${activeSettingsTab === tab.id ? "button-primary" : "button-soft"}`}
            aria-pressed={activeSettingsTab === tab.id}
            onClick={() => {
              setActiveSettingsTab(tab.id);
              setSetupHashTarget(null);
            }}
          >
            {tab.label}
          </button>
        ))}
      </nav>
      <div className={styles.settingsLayout} onChangeCapture={trackFormChange}>
        <ManageAccountSettings
          household={household}
          balances={balances}
          hidden={activeSettingsTab !== "accounts"}
          onRefresh={onRefresh}
          onRefreshBalances={refreshPlanning}
          onNotice={onNotice}
          onFormClean={markFormClean}
          onPendingChange={setAccountSettingsPending}
        />
        <ViewAccountBalanceHistory
          householdId={household.id}
          accounts={household.accounts}
          hidden={activeSettingsTab !== "accounts"}
          refreshRevision={balanceHistoryRevision}
        />
        <section
          className={`panel ${styles.settingsSection}`}
          hidden={activeSettingsTab !== "catalog"}
        >
          <div className={`panel-heading ${styles.panelHeading}`}>
            <div>
              <span className="eyebrow">品目の分類</span>
              <h2>カテゴリ</h2>
            </div>
            <span className="count-badge">
              {household.categories.filter((category) => !category.archived).length}
            </span>
          </div>
          <div className="category-management">
            {household.categories.map((category, index) => (
              <div className="category-management-row" key={category.id}>
                <span
                  className={`category-tag swatch-bg-${index % 6} ${category.archived ? "category-archived" : ""}`}
                >
                  {category.name}
                </span>
                <button
                  className="text-button"
                  disabled={pending}
                  onClick={() => void archiveCategory(category.id, !category.archived)}
                >
                  {category.archived ? "再表示" : "非表示"}
                </button>
              </div>
            ))}
          </div>
          <form className="inline-create" data-unsaved-id="create-category" onSubmit={addCategory}>
            <input
              value={categoryName}
              onChange={(e) => setCategoryName(e.target.value)}
              placeholder="カテゴリ名"
              required
            />
            <button className="button button-soft" disabled={pending}>
              <Plus size={15} />
              追加
            </button>
          </form>
        </section>
        <section
          className={`panel ${styles.settingsSection}`}
          hidden={activeSettingsTab !== "allocation"}
          id="default-allocation-settings"
          style={{ scrollMarginTop: 80 }}
        >
          <div className={`panel-heading ${styles.panelHeading}`}>
            <div>
              <span className="eyebrow">買い物の初期値</span>
              <h2>標準の負担割合</h2>
            </div>
            <span className="count-badge">
              <Activity size={14} />
            </span>
          </div>
          <form data-unsaved-id="default-allocation" onSubmit={saveAllocation}>
            <div className={styles.allocationSettings}>
              {household.pools.map((pool) => (
                <label key={pool.id}>
                  <span>{pool.name}</span>
                  <div className="number-field">
                    <input
                      inputMode="numeric"
                      value={shareWeights[pool.id] ?? ""}
                      onChange={(e) =>
                        setShareWeights((current) => ({
                          ...current,
                          [pool.id]: e.target.value.replace(/\D/g, ""),
                        }))
                      }
                    />
                    <small>比</small>
                  </div>
                </label>
              ))}
            </div>
            <div className={styles.settingsSave}>
              <span>変更後の買い物から適用</span>
              <button
                className={`button button-primary ${styles.settingsSaveButton}`}
                disabled={pending}
              >
                <Check size={16} />
                保存
              </button>
            </div>
          </form>
        </section>
        <ManageAllocationCatalog
          household={household}
          allocationHidden={activeSettingsTab !== "allocation"}
          catalogHidden={activeSettingsTab !== "catalog"}
          onNotice={onNotice}
          onFormClean={markFormClean}
          onPendingChange={setAllocationCatalogPending}
        />
        {isOwner && (
          <ManageHouseholdInvitations
            householdId={household.id}
            hidden={activeSettingsTab !== "members"}
            disabled={pending || invitationPending}
            onNotice={onNotice}
            onPendingChange={setInvitationPending}
            onInvitationFormSaved={() => markFormClean("create-invitation")}
          />
        )}
        <ManageTags
          householdId={household.id}
          hidden={activeSettingsTab !== "catalog"}
          onNotice={onNotice}
          onFormDirty={markFormDirty}
          onFormClean={markFormClean}
          onPendingChange={setTagPending}
        />
        <ManageBudgets
          householdId={household.id}
          categories={household.categories}
          hidden={activeSettingsTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={markFormDirty}
          onFormClean={markFormClean}
          onPendingChange={setBudgetPending}
        />
        <ManageRecurringExpenses
          householdId={household.id}
          categories={household.categories}
          accounts={household.accounts}
          hidden={activeSettingsTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={markFormDirty}
          onFormClean={markFormClean}
          onPendingChange={setRecurringPending}
        />
        <ManageAccountActivity
          household={household}
          hidden={activeSettingsTab !== "accounts"}
          onNotice={onNotice}
          onFormClean={markFormClean}
          onPendingChange={setAccountActivityPending}
          onRecorded={async () => {
            await refreshPlanning();
            setBalanceHistoryRevision((revision) => revision + 1);
          }}
        />
        <section
          className={`panel ${styles.settingsSection} ${styles.memberSection}`}
          hidden={activeSettingsTab !== "members"}
        >
          <div className={`panel-heading ${styles.panelHeading}`}>
            <div>
              <span className="eyebrow">共同編集</span>
              <h2>メンバー</h2>
            </div>
          </div>
          {household.members.map((member) => (
            <div className={styles.settingRow} key={member.id}>
              <div className="profile-avatar">{member.displayName.slice(0, 1)}</div>
              <div>
                <b>{member.displayName}</b>
                <small>{member.role === "OWNER" ? "オーナー" : "メンバー"}</small>
              </div>
            </div>
          ))}
        </section>
      </div>
    </>
  );
}
