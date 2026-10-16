import { Check, RotateCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type SubmitEvent } from "react";

import type { Household } from "../../../shared/api";

import { errorMessage } from "../../../shared/lib";
import { InlineError } from "../../../shared/ui";
import {
  readFundingSchedules,
  setFundingSchedule,
  type FundingSchedule,
} from "../api/funding-schedule-management";

import styles from "./manage-funding-schedules.module.css";

const localDate = () =>
  new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Tokyo",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());

const yen = (amount: number) =>
  new Intl.NumberFormat("ja-JP", {
    style: "currency",
    currency: "JPY",
    maximumFractionDigits: 0,
  }).format(amount);

const cadenceLabel = (value: FundingSchedule["cadence"]) =>
  value === "MONTHLY" ? "毎月" : value === "WEEKLY" ? "毎週" : "指定日";

const isFundingScheduleCadence = (value: string): value is FundingSchedule["cadence"] =>
  value === "MONTHLY" || value === "WEEKLY" || value === "CUSTOM";

export function ManageFundingSchedules({
  household,
  hidden,
  onNotice,
  onFormDirty,
  onFormClean,
  onPendingChange,
}: {
  household: Household;
  hidden: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormDirty: (formID: string) => void;
  onFormClean: (formID: string) => void;
  onPendingChange: (pending: boolean) => void;
}) {
  const [schedules, setSchedules] = useState<FundingSchedule[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [memberId, setMemberId] = useState(household.members[0]?.id ?? "");
  const [poolId, setPoolId] = useState(household.pools[0]?.id ?? "");
  const [amount, setAmount] = useState("");
  const [cadence, setCadence] = useState<FundingSchedule["cadence"]>("MONTHLY");
  const [startsOn, setStartsOn] = useState(localDate);
  const [endsOn, setEndsOn] = useState("");
  const [pending, setPending] = useState(false);
  const requestSequence = useRef(0);

  const refresh = useCallback(async () => {
    const sequence = ++requestSequence.current;
    setLoading(true);
    setLoadError("");
    try {
      const result = await readFundingSchedules(household.id);
      if (requestSequence.current === sequence) setSchedules(result);
    } catch (reason) {
      if (requestSequence.current === sequence) setLoadError(errorMessage(reason));
      throw reason;
    } finally {
      if (requestSequence.current === sequence) setLoading(false);
    }
  }, [household.id]);

  useEffect(() => {
    if (hidden) return;
    queueMicrotask(() => void refresh().catch(() => undefined));
  }, [hidden, refresh]);

  const save = async (event: SubmitEvent<HTMLFormElement>) => {
    event.preventDefault();
    const amountYen = Number(amount);
    if (
      !memberId ||
      !poolId ||
      !Number.isSafeInteger(amountYen) ||
      amountYen < 1 ||
      amountYen > 1_000_000_000_000
    )
      return;
    setPending(true);
    onPendingChange(true);
    try {
      await setFundingSchedule({
        householdId: household.id,
        memberId,
        poolId,
        amountYen,
        cadence,
        startsOn,
        endsOn: cadence === "CUSTOM" || !endsOn ? null : endsOn,
      });
      setAmount("");
      setEndsOn("");
      onFormClean("funding-schedule");
      await refresh().catch(() => undefined);
      onNotice("入金予定を保存しました", "success");
    } catch (reason) {
      onNotice(errorMessage(reason), "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  return (
    <section className={`panel ${styles.section}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">予定</span>
          <h2>入金予定</h2>
        </div>
        <span className="count-badge">
          {schedules.filter((schedule) => schedule.active).length}
        </span>
      </div>
      {loading && <output>入金予定を読み込んでいます</output>}
      {loadError && (
        <div role="alert">
          <InlineError message={loadError} />
          <button
            type="button"
            className="text-button"
            disabled={loading}
            onClick={() => void refresh().catch(() => undefined)}
          >
            <RotateCw size={14} />
            再試行
          </button>
        </div>
      )}
      <form
        data-unsaved-id="funding-schedule"
        onChange={() => onFormDirty("funding-schedule")}
        onSubmit={save}
      >
        <div className={styles.fundingScheduleFormGrid}>
          <label>
            <span>メンバー</span>
            <select value={memberId} onChange={(event) => setMemberId(event.target.value)} required>
              {household.members.map((member) => (
                <option key={member.id} value={member.id}>
                  {member.displayName}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>入金先のプール</span>
            <select value={poolId} onChange={(event) => setPoolId(event.target.value)} required>
              {household.pools.map((pool) => (
                <option key={pool.id} value={pool.id}>
                  {pool.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span>金額</span>
            <input
              inputMode="numeric"
              value={amount}
              onChange={(event) => setAmount(event.target.value.replace(/\D/g, ""))}
              required
            />
          </label>
          <label>
            <span>周期</span>
            <select
              value={cadence}
              onChange={(event) => {
                if (isFundingScheduleCadence(event.target.value)) setCadence(event.target.value);
              }}
            >
              <option value="MONTHLY">毎月</option>
              <option value="WEEKLY">毎週</option>
              <option value="CUSTOM">指定日</option>
            </select>
          </label>
          <label>
            <span>{cadence === "CUSTOM" ? "入金日" : "開始日"}</span>
            <input
              type="date"
              value={startsOn}
              onChange={(event) => setStartsOn(event.target.value)}
              required
            />
          </label>
          {cadence !== "CUSTOM" && (
            <label>
              <span>終了日</span>
              <input
                type="date"
                value={endsOn}
                min={startsOn}
                onChange={(event) => setEndsOn(event.target.value)}
              />
            </label>
          )}
        </div>
        <div className={styles.settingsSave}>
          <button
            className={`button button-primary ${styles.settingsSaveButton}`}
            type="submit"
            disabled={pending || !household.members.length || !household.pools.length}
          >
            <Check size={16} />
            保存
          </button>
        </div>
      </form>
      <div className={styles.settingsRows}>
        {schedules.map((schedule) => (
          <div className={styles.settingRow} key={schedule.id}>
            <div>
              <b>
                {household.members.find((member) => member.id === schedule.memberId)?.displayName ??
                  "メンバー"}{" "}
                · {household.pools.find((pool) => pool.id === schedule.poolId)?.name ?? "プール"}
              </b>
              <small>
                {yen(schedule.amountYen)} · {cadenceLabel(schedule.cadence)} · {schedule.startsOn}
                {schedule.endsOn ? `〜${schedule.endsOn}` : ""} ·{" "}
                {schedule.active ? "有効" : "終了"}
              </small>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
