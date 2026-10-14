import { useState } from "react";

import type { Household } from "../../../shared/api";

import {
  leaveHousehold,
  removeHouseholdMember,
  transferHouseholdOwner,
} from "../api/household-members";

import styles from "./settings-page.module.css";

export function SettingsMembersSection({
  householdId,
  members,
  viewerId,
  isOwner,
  hidden,
  disabled,
  onRefresh,
  onLeave,
  onNotice,
}: {
  householdId: string;
  members: Household["members"];
  viewerId: string | null;
  isOwner: boolean;
  hidden: boolean;
  disabled: boolean;
  onRefresh: () => Promise<void>;
  onLeave: () => void;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
}) {
  const [pending, setPending] = useState(false);

  const handleRemove = async (memberId: string) => {
    if (pending || !window.confirm("このメンバーを家計簿から除名しますか？")) return;
    setPending(true);
    try {
      await removeHouseholdMember({ householdId, memberId });
      await onRefresh();
      onNotice("メンバーを除名しました", "success");
    } catch (error) {
      onNotice(error instanceof Error ? error.message : "メンバーを除名できませんでした", "error");
    } finally {
      setPending(false);
    }
  };

  const handleLeave = async () => {
    if (pending || !window.confirm("この家計簿から脱退しますか？")) return;
    setPending(true);
    try {
      await leaveHousehold({ householdId });
      onLeave();
    } catch (error) {
      onNotice(error instanceof Error ? error.message : "家計簿から脱退できませんでした", "error");
    } finally {
      setPending(false);
    }
  };

  const handleTransfer = async (memberId: string, displayName: string) => {
    if (pending || !window.confirm(`${displayName}にオーナーを移譲しますか？`)) return;
    setPending(true);
    try {
      await transferHouseholdOwner({ householdId, newOwnerId: memberId });
      await onRefresh();
      onNotice(`${displayName}にオーナーを移譲しました`, "success");
    } catch (error) {
      onNotice(error instanceof Error ? error.message : "オーナーを移譲できませんでした", "error");
    } finally {
      setPending(false);
    }
  };

  return (
    <section className={`panel ${styles.settingsSection} ${styles.memberSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">共同編集</span>
          <h2>メンバー</h2>
        </div>
      </div>
      {members.map((member) => (
        <div className={styles.settingRow} key={member.id}>
          <div className="profile-avatar">{member.displayName.slice(0, 1)}</div>
          <div>
            <b>{member.displayName}</b>
            <small>{member.role === "OWNER" ? "オーナー" : "メンバー"}</small>
          </div>
          {member.id === viewerId ? (
            <button
              className="button"
              type="button"
              disabled={disabled || pending}
              onClick={() => void handleLeave()}
            >
              脱退
            </button>
          ) : isOwner ? (
            <>
              <button
                className="button"
                type="button"
                disabled={disabled || pending}
                onClick={() => void handleTransfer(member.id, member.displayName)}
              >
                オーナーを移譲
              </button>
              <button
                className="button"
                type="button"
                disabled={disabled || pending}
                onClick={() => void handleRemove(member.id)}
              >
                除名
              </button>
            </>
          ) : null}
        </div>
      ))}
    </section>
  );
}
