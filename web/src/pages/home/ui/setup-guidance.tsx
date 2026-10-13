import { Link } from "@tanstack/react-router";
import { ArrowRight, Circle } from "lucide-react";

import type { Household } from "../../../shared/api";

import styles from "./setup-guidance.module.css";

type SetupStep = {
  destination: "settings" | "new";
  action: string;
  hash?: string;
};

export function SetupGuidance({
  household,
  purchaseCount,
  onNavigateToPurchase,
}: {
  household: Household;
  purchaseCount: number;
  onNavigateToPurchase: () => void;
}) {
  const poolsConfigured =
    household.pools.length > 0 &&
    household.pools.every((pool) => !pool.__typename.startsWith("Unconfigured"));
  const defaultAllocationConfigured =
    household.pools.length > 0 &&
    household.defaultAllocation.length === household.pools.length &&
    household.defaultAllocation.every((allocation) => allocation.weight > 0);
  const steps: SetupStep[] = [
    ...(!poolsConfigured
      ? [
          {
            destination: "settings" as const,
            action: "負担先を設定",
            hash: "pool-settings",
          },
        ]
      : []),
    ...(household.accounts.length === 0
      ? [
          {
            destination: "settings" as const,
            action: "決済元を登録",
            hash: "payment-source-settings",
          },
        ]
      : []),
    ...(!defaultAllocationConfigured
      ? [
          {
            destination: "settings" as const,
            action: "標準の負担割合を決める",
            hash: "default-allocation-settings",
          },
        ]
      : []),
    ...(purchaseCount === 0
      ? [
          {
            destination: "new" as const,
            action: "買い物を記録",
          },
        ]
      : []),
  ];

  if (steps.length === 0) return null;

  return (
    <section className={`panel ${styles.panel}`} aria-labelledby="setup-guidance-title">
      <div className={styles.heading}>
        <h2 id="setup-guidance-title">初期設定</h2>
        <span className={styles.count} aria-label={`未完了${steps.length}件`}>
          {steps.length}
        </span>
      </div>
      <ol className={styles.steps}>
        {steps.map((step) => (
          <li className={styles.step} key={step.action}>
            <Circle className={styles.status} size={17} aria-hidden="true" />
            {step.destination === "settings" ? (
              <Link
                className={`button button-soft ${styles.action}`}
                to="/households/$householdId/settings"
                params={{ householdId: household.id }}
                hash={step.hash}
                search
                aria-label={step.action}
              >
                {step.action}
                <ArrowRight size={15} aria-hidden="true" />
              </Link>
            ) : (
              <Link
                className={`button button-soft ${styles.action}`}
                to="/households/$householdId/new"
                params={{ householdId: household.id }}
                search
                onClick={onNavigateToPurchase}
                aria-label={step.action}
              >
                {step.action}
                <ArrowRight size={15} aria-hidden="true" />
              </Link>
            )}
          </li>
        ))}
      </ol>
    </section>
  );
}
