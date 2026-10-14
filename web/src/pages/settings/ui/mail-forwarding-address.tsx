import { Check, Copy } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { errorMessage } from "../../../shared/lib";
import {
  issueMailForwardingAddress,
  readMailForwardingStatus,
  revokeMailForwardingAddress,
  rotateMailForwardingAddress,
  type IssuedMailForwardingAddress,
  type MailForwardingAddressStatus,
} from "../api/mail-forwarding";

import styles from "./mail-forwarding-address.module.css";

export function MailForwardingAddress({
  householdId,
  hidden,
  disabled,
  onPendingChange,
  onNotice,
}: {
  householdId: string;
  hidden: boolean;
  disabled: boolean;
  onPendingChange: (pending: boolean) => void;
  onNotice: (message: string, kind?: "success" | "error") => void;
}) {
  const [status, setStatus] = useState<MailForwardingAddressStatus | null>(null);
  const [statusHouseholdId, setStatusHouseholdId] = useState("");
  const [issued, setIssued] = useState<IssuedMailForwardingAddress | null>(null);
  const [pending, setPending] = useState(false);
  const [copied, setCopied] = useState(false);
  const onNoticeRef = useRef(onNotice);

  useEffect(() => {
    onNoticeRef.current = onNotice;
  }, [onNotice]);

  useEffect(() => {
    let current = true;
    void readMailForwardingStatus(householdId)
      .then((result) => {
        if (!current) return;
        setStatus(result);
        setStatusHouseholdId(householdId);
        setIssued(null);
      })
      .catch((reason) => {
        if (current) onNoticeRef.current(errorMessage(reason), "error");
      });
    return () => {
      current = false;
    };
  }, [householdId]);

  const activeStatus = statusHouseholdId === householdId ? status : null;
  const statusLoaded = statusHouseholdId === householdId;

  const run = async (action: () => Promise<void>) => {
    setPending(true);
    onPendingChange(true);
    try {
      await action();
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    } finally {
      setPending(false);
      onPendingChange(false);
    }
  };

  const issue = (action: (householdId: string) => Promise<IssuedMailForwardingAddress>) =>
    run(async () => {
      const result = await action(householdId);
      setIssued(result);
      setStatus({ active: true, createdAt: result.createdAt });
      setStatusHouseholdId(householdId);
      setCopied(false);
      onNoticeRef.current("転送先アドレスを発行しました", "success");
    });

  const revoke = () =>
    run(async () => {
      await revokeMailForwardingAddress(householdId);
      setIssued(null);
      setStatus(null);
      setStatusHouseholdId(householdId);
      onNoticeRef.current("転送先アドレスを無効化しました", "success");
    });

  const copyAddress = async () => {
    if (!issued) return;
    try {
      await navigator.clipboard.writeText(issued.address);
      setCopied(true);
      onNoticeRef.current("転送先アドレスをコピーしました", "success");
    } catch (reason) {
      onNoticeRef.current(errorMessage(reason), "error");
    }
  };

  return (
    <section className={`panel ${styles.section}`} hidden={hidden}>
      <div className={`panel-heading ${styles.heading}`}>
        <div>
          <span className="eyebrow">購入メール</span>
          <h2>転送先</h2>
        </div>
      </div>
      <p>表示されたアドレスを購入メールの転送先に設定してください。</p>
      {activeStatus ? (
        <div className={styles.status}>
          <span>発行済み</span>
          <time dateTime={activeStatus.createdAt}>
            {new Date(activeStatus.createdAt).toLocaleString("ja-JP")}
          </time>
        </div>
      ) : (
        <p>未発行</p>
      )}
      {issued && (
        <div className={styles.address}>
          <label htmlFor="mail-forwarding-address">転送先アドレス</label>
          <div className={styles.addressControls}>
            <input id="mail-forwarding-address" readOnly value={issued.address} />
            <button
              className="button button-soft"
              type="button"
              disabled={disabled || pending}
              onClick={() => void copyAddress()}
            >
              {copied ? (
                <Check size={16} aria-hidden="true" />
              ) : (
                <Copy size={16} aria-hidden="true" />
              )}
              {copied ? "コピー済み" : "アドレスをコピー"}
            </button>
          </div>
        </div>
      )}
      <div className={styles.actions}>
        {activeStatus ? (
          <>
            <button
              className="button button-soft"
              type="button"
              disabled={disabled || pending || !statusLoaded}
              onClick={() => void issue(rotateMailForwardingAddress)}
            >
              転送先を再発行
            </button>
            <button
              className="button button-soft"
              type="button"
              disabled={disabled || pending || !statusLoaded}
              onClick={() => void revoke()}
            >
              転送先を無効化
            </button>
          </>
        ) : (
          <button
            className="button button-primary"
            type="button"
            disabled={disabled || pending || !statusLoaded}
            onClick={() => void issue(issueMailForwardingAddress)}
          >
            転送先アドレスを発行
          </button>
        )}
      </div>
    </section>
  );
}
