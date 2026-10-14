import { useState, type SubmitEvent } from "react";

import type { Household } from "../../../shared/api";

import { errorMessage } from "../../../shared/lib";
import {
  createCategory as createCategoryMutation,
  setCategoryArchived as setCategoryArchivedMutation,
  updateCategoryName as updateCategoryNameMutation,
  setDefaultAllocation as setDefaultAllocationMutation,
} from "../api/category-management";

export function useSettingsMutations({
  household,
  onRefresh,
  onNotice,
  markFormClean,
  setPending,
}: {
  household: Household;
  onRefresh: () => Promise<void>;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  markFormClean: (formID: string) => void;
  setPending: (operation: string, pending: boolean) => void;
}) {
  const [categoryName, setCategoryName] = useState("");
  const [shareWeights, setShareWeights] = useState<Record<string, string>>(() =>
    Object.fromEntries(
      household.pools.map((pool) => [pool.id, String(pool.defaultShareWeight || 1)]),
    ),
  );

  const saveMutation = async (mutation: () => Promise<void>, formID: string): Promise<boolean> => {
    setPending("settings", true);
    try {
      await mutation();
      await onRefresh();
      markFormClean(formID);
      onNotice("設定を保存しました", "success");
      return true;
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
      return false;
    } finally {
      setPending("settings", false);
    }
  };

  const addCategory = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    void saveMutation(
      () => createCategoryMutation({ householdId: household.id, name: categoryName.trim() }),
      "create-category",
    ).then((saved) => saved && setCategoryName(""));
  };

  const saveAllocation = (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    void saveMutation(
      () =>
        setDefaultAllocationMutation({
          householdId: household.id,
          allocations: household.pools
            .map((pool) => ({
              poolId: pool.id,
              weight: Math.max(0, Number(shareWeights[pool.id]) || 0),
            }))
            .filter((item) => item.weight > 0),
        }),
      "default-allocation",
    );
  };

  const archiveCategory = async (categoryId: string, archived: boolean) => {
    setPending("settings", true);
    try {
      await setCategoryArchivedMutation({ categoryId, archived });
      await onRefresh();
      onNotice(archived ? "カテゴリを非表示にしました" : "カテゴリを再表示しました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
    } finally {
      setPending("settings", false);
    }
  };

  const renameCategory = async (categoryId: string, name: string): Promise<boolean> => {
    setPending("settings", true);
    try {
      await updateCategoryNameMutation({ categoryId, name });
      await onRefresh();
      onNotice("カテゴリ名を保存しました", "success");
      return true;
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
      return false;
    } finally {
      setPending("settings", false);
    }
  };

  return {
    addCategory,
    archiveCategory,
    categoryName,
    renameCategory,
    saveAllocation,
    setCategoryName,
    setShareWeights,
    shareWeights,
  };
}
