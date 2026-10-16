import { useEffect } from "react";

import type { Household } from "../../../shared/api";

import { useSettingsAccountBalances } from "../model/account-balances";
import { useAccountSettings } from "../model/account-settings";
import { useAllocationCatalog } from "../model/allocation-catalog";
import { useSettingsChangeTracking } from "../model/settings-change-tracking";
import { useSettingsMutations } from "../model/settings-mutations";
import { useSettingsNavigation, type SettingsTab } from "../model/settings-navigation";
import { MailForwardingAddress } from "./mail-forwarding-address";
import { ManageAccountActivity } from "./manage-account-activity";
import { ManageAccountSettings } from "./manage-account-settings";
import { ManageAllocationCatalog } from "./manage-allocation-catalog";
import { ManageBudgets } from "./manage-budgets";
import { ManageCategoriesSection } from "./manage-categories-section";
import { ManageDefaultAllocationSection } from "./manage-default-allocation-section";
import { ManageFundingSchedules } from "./manage-funding-schedules";
import { ManageHouseholdInvitations } from "./manage-household-invitations";
import { ManageRecurringExpenses } from "./manage-recurring-expenses";
import { ManageTags } from "./manage-tags";
import { SettingsMembersSection } from "./settings-members-section";
import { SettingsTabs } from "./settings-tabs";
import { ViewAccountBalanceHistory } from "./view-account-balance-history";

import styles from "./settings-page.module.css";

export function SettingsPage({
  household,
  viewerId,
  onLeave,
  onRefresh,
  onNotice,
  onUnsavedChanges,
  isOwner,
  activeTab,
  onSelectTab,
}: {
  household: Household;
  viewerId: string | null;
  onLeave: () => void;
  onRefresh: () => Promise<void>;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onUnsavedChanges: (unsaved: boolean) => void;
  isOwner: boolean;
  activeTab: SettingsTab;
  onSelectTab: (tab: SettingsTab) => void;
}) {
  const navigation = useSettingsNavigation(activeTab, onSelectTab);
  const changes = useSettingsChangeTracking();
  const balances = useSettingsAccountBalances({
    household,
    onNotice,
    enabled: activeTab === "accounts",
  });
  const accountSettings = useAccountSettings({
    household,
    onRefresh,
    onRefreshBalances: balances.refresh,
    onNotice,
    onFormClean: changes.markFormClean,
  });
  const allocationCatalog = useAllocationCatalog({
    household,
    onNotice,
    onFormClean: changes.markFormClean,
    enabled: activeTab === "allocation" || activeTab === "catalog",
  });
  const mutations = useSettingsMutations({
    household,
    onRefresh,
    onNotice,
    markFormClean: changes.markFormClean,
    setPending: changes.setPending,
  });
  const isPending = changes.isPending;

  const overallPending =
    isPending("settings") || accountSettings.pending || allocationCatalog.pending;
  const hasUnsavedChanges = changes.hasUnsavedChanges || overallPending;

  useEffect(() => {
    onUnsavedChanges(hasUnsavedChanges);
    return () => onUnsavedChanges(false);
  }, [hasUnsavedChanges, onUnsavedChanges]);

  return (
    <>
      <div className="page-heading">
        <div>
          <span className="eyebrow">共有と分類</span>
          <h1>設定</h1>
          <p>{household.name}の口座、決済元、カテゴリ</p>
        </div>
      </div>
      <SettingsTabs activeTab={activeTab} onSelect={navigation.selectTab} />
      <div className={styles.settingsLayout} onChangeCapture={changes.trackFormChange}>
        <ManageAccountSettings
          household={household}
          balances={balances.balances}
          hidden={activeTab !== "accounts"}
          model={accountSettings}
        />
        <ViewAccountBalanceHistory
          householdId={household.id}
          accounts={household.accounts}
          hidden={activeTab !== "accounts"}
          refreshRevision={balances.historyRevision}
        />
        <ManageCategoriesSection
          categories={household.categories}
          categoryName={mutations.categoryName}
          disabled={isPending("settings")}
          hidden={activeTab !== "catalog"}
          onCategoryNameChange={mutations.setCategoryName}
          onAdd={mutations.addCategory}
          onArchive={(categoryId, archived) => void mutations.archiveCategory(categoryId, archived)}
          onRename={mutations.renameCategory}
          onFormDirty={changes.markFormDirty}
          onFormClean={changes.markFormClean}
        />
        <ManageDefaultAllocationSection
          pools={household.pools}
          shareWeights={mutations.shareWeights}
          disabled={isPending("settings")}
          hidden={activeTab !== "allocation"}
          onShareWeightChange={(poolId, weight) =>
            mutations.setShareWeights((current) => ({ ...current, [poolId]: weight }))
          }
          onSave={mutations.saveAllocation}
        />
        <ManageAllocationCatalog
          household={household}
          allocationHidden={activeTab !== "allocation"}
          catalogHidden={activeTab !== "catalog"}
          model={allocationCatalog}
        />
        {isOwner && (
          <ManageHouseholdInvitations
            householdId={household.id}
            hidden={activeTab !== "members"}
            disabled={isPending("settings") || isPending("invitations")}
            onNotice={onNotice}
            onPendingChange={(pending) => changes.setPending("invitations", pending)}
            onInvitationFormSaved={() => changes.markFormClean("create-invitation")}
          />
        )}
        {isOwner && (
          <MailForwardingAddress
            householdId={household.id}
            hidden={activeTab !== "members"}
            disabled={isPending("settings") || isPending("mail-forwarding")}
            onNotice={onNotice}
            onPendingChange={(pending) => changes.setPending("mail-forwarding", pending)}
          />
        )}
        <ManageTags
          householdId={household.id}
          hidden={activeTab !== "catalog"}
          onNotice={onNotice}
          onFormDirty={changes.markFormDirty}
          onFormClean={changes.markFormClean}
          onPendingChange={(pending) => changes.setPending("tags", pending)}
        />
        <ManageBudgets
          householdId={household.id}
          categories={household.categories}
          hidden={activeTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={changes.markFormDirty}
          onFormClean={changes.markFormClean}
          onPendingChange={(pending) => changes.setPending("budgets", pending)}
        />
        <ManageRecurringExpenses
          householdId={household.id}
          categories={household.categories}
          accounts={household.accounts}
          hidden={activeTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={changes.markFormDirty}
          onFormClean={changes.markFormClean}
          onPendingChange={(pending) => changes.setPending("recurring-expenses", pending)}
        />
        <ManageFundingSchedules
          household={household}
          hidden={activeTab !== "planning"}
          onNotice={onNotice}
          onFormDirty={changes.markFormDirty}
          onFormClean={changes.markFormClean}
          onPendingChange={(pending) => changes.setPending("funding-schedule", pending)}
        />
        <ManageAccountActivity
          household={household}
          hidden={activeTab !== "accounts"}
          onNotice={onNotice}
          onFormClean={changes.markFormClean}
          onPendingChange={(pending) => changes.setPending("account-activity", pending)}
          onRecorded={balances.refreshAfterActivity}
        />
        <SettingsMembersSection
          householdId={household.id}
          members={household.members}
          viewerId={viewerId}
          isOwner={isOwner}
          hidden={activeTab !== "members"}
          disabled={isPending("settings")}
          onRefresh={onRefresh}
          onLeave={onLeave}
          onNotice={onNotice}
        />
      </div>
    </>
  );
}
