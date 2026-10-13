import type { FormEvent } from "react";

import { Activity, Check } from "lucide-react";

import type { Household } from "../../../shared/api";

import styles from "./settings-page.module.css";

export function ManageDefaultAllocationSection({
  pools,
  shareWeights,
  disabled,
  hidden,
  onShareWeightChange,
  onSave,
}: {
  pools: Household["pools"];
  shareWeights: Record<string, string>;
  disabled: boolean;
  hidden: boolean;
  onShareWeightChange: (poolId: string, weight: string) => void;
  onSave: (event: FormEvent) => void;
}) {
  return (
    <section
      className={`panel ${styles.settingsSection}`}
      hidden={hidden}
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
      <form data-unsaved-id="default-allocation" onSubmit={onSave}>
        <div className={styles.allocationSettings}>
          {pools.map((pool) => (
            <label key={pool.id}>
              <span>{pool.name}</span>
              <div className="number-field">
                <input
                  inputMode="numeric"
                  value={shareWeights[pool.id] ?? ""}
                  onChange={(event) =>
                    onShareWeightChange(pool.id, event.target.value.replace(/\D/g, ""))
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
            disabled={disabled}
          >
            <Check size={16} />
            保存
          </button>
        </div>
      </form>
    </section>
  );
}
