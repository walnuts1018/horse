import type { Account } from "../../../shared/api";
import type { StatementEntry } from "../model/imports";

type Candidate = { id: string; description: string; occurredAt: string };

type Props = {
  entry: StatementEntry;
  accounts: Account[];
  pending: boolean;
  confirmed: boolean;
  issue?: string;
  accountId: string;
  selectedIncomeId: string;
  matchingIncomes: Candidate[];
  hasMoreIncomeCandidates: boolean;
  loadingIncomeCandidates: boolean;
  incomePageError: boolean;
  onSelectAccount: (accountId: string) => void;
  onSelectIncome: (incomeId: string) => void;
  onLoadMoreIncomes: () => void;
  onConfirm: (entry: StatementEntry, accountId: string) => void;
  onLinkIncome: (entry: StatementEntry, accountId: string, incomeId: string) => void;
};

export function PaymentEntryEditor({
  entry,
  accounts,
  pending,
  confirmed,
  issue,
  accountId,
  selectedIncomeId,
  matchingIncomes,
  hasMoreIncomeCandidates,
  loadingIncomeCandidates,
  incomePageError,
  onSelectAccount,
  onSelectIncome,
  onLoadMoreIncomes,
  onConfirm,
  onLinkIncome,
}: Props) {
  const isExternalIncome = entry.kind === "EXTERNAL_INCOME";
  if (issue) return <small>{issue}</small>;
  if (confirmed) return <small>{isExternalIncome ? "入金登録済み" : "追加済み"}</small>;

  return (
    <>
      <label>
        <span>{isExternalIncome ? "入金先の銀行口座" : "決済口座"}</span>
        <select
          value={accountId}
          disabled={pending}
          onChange={(event) => onSelectAccount(event.target.value)}
        >
          <option value="">口座を選択</option>
          {accounts
            .filter((account) => !isExternalIncome || account.kind === "BANK")
            .map((account) => (
              <option key={account.id} value={account.id}>
                {account.name}
              </option>
            ))}
        </select>
      </label>
      {isExternalIncome && matchingIncomes.length > 0 && (
        <label>
          <span>既存の入金</span>
          <select
            value={selectedIncomeId}
            disabled={pending}
            onChange={(event) => onSelectIncome(event.target.value)}
          >
            <option value="">新しい入金として記録</option>
            {matchingIncomes.map((income) => (
              <option key={income.id} value={income.id}>
                {income.occurredAt.slice(0, 10)} · {income.description}
              </option>
            ))}
          </select>
        </label>
      )}
      {isExternalIncome && hasMoreIncomeCandidates && (
        <button
          type="button"
          className="text-button"
          disabled={loadingIncomeCandidates}
          onClick={onLoadMoreIncomes}
        >
          {loadingIncomeCandidates ? "読み込み中" : "過去の入金を読み込む"}
        </button>
      )}
      {isExternalIncome && incomePageError && (
        <p className="inline-error" role="alert">
          過去の入金を読み込めませんでした
          <button type="button" className="text-button" onClick={onLoadMoreIncomes}>
            再試行
          </button>
        </p>
      )}
      <button
        type="button"
        className="text-button"
        disabled={pending || !accountId}
        onClick={() =>
          selectedIncomeId
            ? onLinkIncome(entry, accountId, selectedIncomeId)
            : onConfirm(entry, accountId)
        }
      >
        {isExternalIncome ? (selectedIncomeId ? "既存の入金に紐付け" : "入金を記録") : "購入に追加"}
      </button>
    </>
  );
}
