import styles from "./settings-page.module.css";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Plus } from "lucide-react";
import {
  readGraphql,
  type CreateHouseholdInvitationMutation$data,
  type HouseholdInvitationsQuery$data,
  type RevokeHouseholdInvitationMutation$data,
} from "../../../shared/api";
import {
  createHouseholdInvitationMutation,
  householdInvitationsQuery,
  revokeHouseholdInvitationMutation,
} from "../api/household-invitations";

type Invitation = HouseholdInvitationsQuery$data["householdInvitations"]["edges"][number]["node"];

export function ManageHouseholdInvitations({
  householdId,
  hidden,
  disabled,
  onNotice,
  onPendingChange,
  onInvitationFormSaved,
}: {
  householdId: string;
  hidden: boolean;
  disabled: boolean;
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onPendingChange: (pending: boolean) => void;
  onInvitationFormSaved: () => void;
}) {
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [invitationsCursor, setInvitationsCursor] = useState<string | null>(null);
  const [invitationsHasNextPage, setInvitationsHasNextPage] = useState(false);
  const [invitationsPageLoading, setInvitationsPageLoading] = useState(false);
  const [invitationLink, setInvitationLink] = useState("");
  const [issuedInvitationId, setIssuedInvitationId] = useState("");
  const [expiresInDays, setExpiresInDays] = useState("7");

  const refreshInvitations = useCallback(async () => {
    const result = await readGraphql<HouseholdInvitationsQuery$data>(householdInvitationsQuery, {
      householdId,
      first: 50,
    });
    setInvitations(result.householdInvitations.edges.map((edge) => edge.node));
    setInvitationsCursor(result.householdInvitations.pageInfo.endCursor ?? null);
    setInvitationsHasNextPage(result.householdInvitations.pageInfo.hasNextPage);
  }, [householdId]);

  const loadMoreInvitations = async () => {
    if (!invitationsCursor || invitationsPageLoading) return;
    setInvitationsPageLoading(true);
    try {
      const result = await readGraphql<HouseholdInvitationsQuery$data>(householdInvitationsQuery, {
        householdId,
        after: invitationsCursor,
        first: 50,
      });
      setInvitations((current) => [
        ...current,
        ...result.householdInvitations.edges.map((edge) => edge.node),
      ]);
      setInvitationsCursor(result.householdInvitations.pageInfo.endCursor ?? null);
      setInvitationsHasNextPage(result.householdInvitations.pageInfo.hasNextPage);
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      setInvitationsPageLoading(false);
    }
  };

  useEffect(() => {
    setInvitationLink("");
    setIssuedInvitationId("");
    void refreshInvitations().catch((reason) => onNotice((reason as Error).message, "error"));
  }, [householdId, onNotice, refreshInvitations]);

  const issueInvitation = async (event: FormEvent) => {
    event.preventDefault();
    onPendingChange(true);
    try {
      const expiresAt = new Date(Date.now() + Number(expiresInDays) * 86_400_000).toISOString();
      const result = await readGraphql<CreateHouseholdInvitationMutation$data>(
        createHouseholdInvitationMutation,
        { input: { householdId, expiresAt } },
      );
      const created = result.createHouseholdInvitation as unknown as NonNullable<
        CreateHouseholdInvitationMutation$data["createHouseholdInvitation"]["result"]
      >;
      onInvitationFormSaved();
      setInvitationLink(`${window.location.origin}/#invite=${encodeURIComponent(created.token)}`);
      setIssuedInvitationId(created.invitation.id);
      await refreshInvitations();
      onNotice("招待を発行しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      onPendingChange(false);
    }
  };

  const revokeInvitation = async (invitationId: string) => {
    onPendingChange(true);
    try {
      await readGraphql<RevokeHouseholdInvitationMutation$data>(revokeHouseholdInvitationMutation, {
        input: { householdId, invitationId },
      });
      if (invitationId === issuedInvitationId) {
        setInvitationLink("");
        setIssuedInvitationId("");
      }
      await refreshInvitations();
      onNotice("招待を失効しました", "success");
    } catch (reason) {
      onNotice((reason as Error).message, "error");
    } finally {
      onPendingChange(false);
    }
  };

  return (
    <section className={`panel ${styles.settingsSection}`} hidden={hidden}>
      <div className={`panel-heading ${styles.panelHeading}`}>
        <div>
          <span className="eyebrow">共同編集</span>
          <h2>招待</h2>
        </div>
        <span className="count-badge">
          {
            invitations.filter(
              (invitation) => invitation.__typename === "PendingHouseholdInvitation",
            ).length
          }
        </span>
      </div>
      <form
        className="inline-create"
        data-unsaved-id="create-invitation"
        onSubmit={(event) => void issueInvitation(event)}
      >
        <label>
          有効期間
          <select value={expiresInDays} onChange={(event) => setExpiresInDays(event.target.value)}>
            <option value="1">1日</option>
            <option value="7">7日</option>
            <option value="30">30日</option>
          </select>
        </label>
        <button className="button button-primary" disabled={disabled}>
          <Plus size={15} />
          招待リンクを発行
        </button>
      </form>
      {invitationLink && (
        <div className={styles.invitationIssued}>
          <input aria-label="招待リンク" readOnly value={invitationLink} />
          <button
            type="button"
            className="button button-soft"
            onClick={() =>
              void navigator.clipboard
                .writeText(invitationLink)
                .then(() => onNotice("招待リンクをコピーしました", "success"))
                .catch(() => onNotice("招待リンクをコピーできませんでした", "error"))
            }
          >
            リンクをコピー
          </button>
        </div>
      )}
      <div className={styles.settingsRows}>
        {invitations.map((invitation) => (
          <div className={styles.settingRow} key={invitation.id}>
            <div className={`pool-avatar ${styles.poolAvatar}`}>招</div>
            <div>
              <b>{new Date(invitation.expiresAt).toLocaleDateString("ja-JP")}まで</b>
              <small>
                {invitation.__typename === "AcceptedHouseholdInvitation"
                  ? "参加済み"
                  : invitation.__typename === "RevokedHouseholdInvitation"
                    ? "失効"
                    : invitation.__typename === "ExpiredHouseholdInvitation"
                      ? "期限切れ"
                      : invitation.__typename === "PendingHouseholdInvitation"
                        ? "未使用"
                        : "状態不明"}
              </small>
            </div>
            {invitation.__typename === "PendingHouseholdInvitation" && (
              <button
                className="text-button"
                disabled={disabled}
                onClick={() => void revokeInvitation(invitation.id)}
              >
                失効
              </button>
            )}
          </div>
        ))}
      </div>
      {invitationsHasNextPage && (
        <button
          className="text-button"
          type="button"
          disabled={invitationsPageLoading}
          onClick={() => void loadMoreInvitations()}
        >
          {invitationsPageLoading ? "読み込み中" : "さらに表示"}
        </button>
      )}
    </section>
  );
}
