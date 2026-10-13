import { useEffect, useRef, useState, type FormEvent } from "react";
import { makeIdempotencyKey, MutationUserError } from "../../../shared/api";
import {
  clearAccountIncomeIntent,
  clearAccountMovementIntent,
  getAccountIncomeIntent,
  getAccountMovementIntent,
  saveAccountIncomeIntent,
  saveAccountMovementIntent,
  type AccountIncomeIntent,
  type AccountMovementIntent,
} from "./pending-account-mutations";
import {
  readAccountActivity,
  readMoreAccountIncomes,
  readMoreAccountMovements,
  recordAccountActivityIncome,
  recordAccountActivityMovement,
  type AccountActivityIncome,
  type AccountActivityMovement,
} from "../api/account-activity";

const localDateTime = () => {
  const date = new Date(Date.now() - new Date().getTimezoneOffset() * 60_000);
  return date.toISOString().slice(0, 16);
};

const localDateTimeFromIso = (value: string) => {
  const date = new Date(Date.parse(value) - new Date().getTimezoneOffset() * 60_000);
  return date.toISOString().slice(0, 16);
};

export function useAccountActivity({
  householdId,
  accounts,
  onNotice,
  onFormClean,
  onRecorded,
}: {
  householdId: string;
  accounts: { id: string; kind: string }[];
  onNotice: (message: string, kind?: "success" | "error" | "warning" | "info") => void;
  onFormClean: (formID: string) => void;
  onRecorded: () => void | Promise<void>;
}) {
  const onNoticeRef = useRef(onNotice);
  const [movements, setMovements] = useState<AccountActivityMovement[]>([]);
  const [incomes, setIncomes] = useState<AccountActivityIncome[]>([]);
  const [movementCursor, setMovementCursor] = useState<string | null>(null);
  const [incomeCursor, setIncomeCursor] = useState<string | null>(null);
  const [movementHasNextPage, setMovementHasNextPage] = useState(false);
  const [incomeHasNextPage, setIncomeHasNextPage] = useState(false);
  const [movementPageLoading, setMovementPageLoading] = useState(false);
  const [incomePageLoading, setIncomePageLoading] = useState(false);
  const [movementPageError, setMovementPageError] = useState("");
  const [incomePageError, setIncomePageError] = useState("");
  const [incomeAccountId, setIncomeAccountId] = useState("");
  const [incomeAmount, setIncomeAmount] = useState("");
  const [incomeDescription, setIncomeDescription] = useState("");
  const [incomeDate, setIncomeDate] = useState(localDateTime);
  const [movementKind, setMovementKind] = useState<"TRANSFER" | "CARD_BILL_PAYMENT">("TRANSFER");
  const [movementFrom, setMovementFrom] = useState("");
  const [movementTo, setMovementTo] = useState("");
  const [movementAmount, setMovementAmount] = useState("");
  const [movementDescription, setMovementDescription] = useState("");
  const [movementDate, setMovementDate] = useState(localDateTime);
  const [pendingIncomeIntent, setPendingIncomeIntent] = useState<AccountIncomeIntent | null>(null);
  const [pendingMovementIntent, setPendingMovementIntent] = useState<AccountMovementIntent | null>(
    null,
  );
  const [pending, setPending] = useState(false);

  useEffect(() => {
    onNoticeRef.current = onNotice;
  }, [onNotice]);

  const refresh = async () => {
    const result = await readAccountActivity(householdId);
    setMovements(result.movements.edges.map((edge) => edge.node));
    setMovementCursor(result.movements.pageInfo.endCursor);
    setMovementHasNextPage(result.movements.pageInfo.hasNextPage);
    setIncomes(result.incomes.edges.map((edge) => edge.node));
    setIncomeCursor(result.incomes.pageInfo.endCursor);
    setIncomeHasNextPage(result.incomes.pageInfo.hasNextPage);
  };

  useEffect(() => {
    void refresh().catch((reason) => onNoticeRef.current((reason as Error).message, "error"));
    const incomeIntent = getAccountIncomeIntent(householdId);
    if (incomeIntent) {
      setPendingIncomeIntent(incomeIntent);
      setIncomeAccountId(incomeIntent.input.accountId);
      setIncomeAmount(String(incomeIntent.input.amountYen));
      setIncomeDate(localDateTimeFromIso(incomeIntent.input.occurredAt));
      setIncomeDescription(incomeIntent.input.description);
    } else {
      setPendingIncomeIntent(null);
    }
    const movementIntent = getAccountMovementIntent(householdId);
    if (movementIntent) {
      setPendingMovementIntent(movementIntent);
      setMovementKind(movementIntent.input.kind);
      setMovementFrom(movementIntent.input.fromAccountId);
      setMovementTo(movementIntent.input.toAccountId);
      setMovementAmount(String(movementIntent.input.amountYen));
      setMovementDate(localDateTimeFromIso(movementIntent.input.occurredAt));
      setMovementDescription(movementIntent.input.description);
    } else {
      setPendingMovementIntent(null);
    }
  }, [householdId]);

  useEffect(() => {
    const sourceAccounts = accounts.filter((account) =>
      movementKind === "TRANSFER" ? account.kind !== "CARD" : account.kind === "BANK",
    );
    const destinationAccounts = accounts.filter((account) =>
      movementKind === "TRANSFER" ? account.kind !== "CARD" : account.kind === "CARD",
    );
    if (!sourceAccounts.some((account) => account.id === movementFrom)) {
      setMovementFrom(sourceAccounts[0]?.id ?? "");
    }
    if (!destinationAccounts.some((account) => account.id === movementTo)) {
      setMovementTo(destinationAccounts[0]?.id ?? "");
    }
  }, [accounts, movementKind, movementFrom, movementTo]);

  const loadMoreMovements = async () => {
    if (!movementCursor || movementPageLoading) return;
    setMovementPageLoading(true);
    setMovementPageError("");
    try {
      const result = await readMoreAccountMovements(householdId, movementCursor);
      setMovements((current) => [...current, ...result.edges.map((edge) => edge.node)]);
      setMovementCursor(result.pageInfo.endCursor);
      setMovementHasNextPage(result.pageInfo.hasNextPage);
    } catch {
      setMovementPageError("口座移動履歴を読み込めませんでした。");
    } finally {
      setMovementPageLoading(false);
    }
  };

  const loadMoreIncomes = async () => {
    if (!incomeCursor || incomePageLoading) return;
    setIncomePageLoading(true);
    setIncomePageError("");
    try {
      const result = await readMoreAccountIncomes(householdId, incomeCursor);
      setIncomes((current) => [...current, ...result.edges.map((edge) => edge.node)]);
      setIncomeCursor(result.pageInfo.endCursor);
      setIncomeHasNextPage(result.pageInfo.hasNextPage);
    } catch {
      setIncomePageError("入金履歴を読み込めませんでした。");
    } finally {
      setIncomePageLoading(false);
    }
  };

  const saveIncome = async (event: FormEvent) => {
    event.preventDefault();
    if (!pendingIncomeIntent && (!incomeAccountId || !Number(incomeAmount))) return;
    setPending(true);
    try {
      const intent =
        pendingIncomeIntent ??
        (() => {
          const idempotencyKey = makeIdempotencyKey();
          return {
            idempotencyKey,
            input: {
              householdId,
              accountId: incomeAccountId,
              amountYen: Number(incomeAmount),
              occurredAt: new Date(incomeDate).toISOString(),
              description: incomeDescription.trim(),
              idempotencyKey,
            },
          };
        })();
      saveAccountIncomeIntent(householdId, intent);
      setPendingIncomeIntent(intent);
      await recordAccountActivityIncome(intent.input);
      clearAccountIncomeIntent(householdId);
      setPendingIncomeIntent(null);
      onFormClean("account-income");
      setIncomeAmount("");
      setIncomeDescription("");
      setIncomeDate(localDateTime());
      await refresh();
      await onRecorded();
      onNoticeRef.current("入金を記録しました", "success");
    } catch (reason) {
      if (reason instanceof MutationUserError) {
        clearAccountIncomeIntent(householdId);
        setPendingIncomeIntent(null);
      }
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };

  const saveMovement = async (event: FormEvent) => {
    event.preventDefault();
    if (
      !pendingMovementIntent &&
      (!movementFrom || !movementTo || movementFrom === movementTo || !Number(movementAmount))
    )
      return;
    setPending(true);
    try {
      const intent =
        pendingMovementIntent ??
        (() => {
          const idempotencyKey = makeIdempotencyKey();
          return {
            idempotencyKey,
            input: {
              householdId,
              kind: movementKind,
              fromAccountId: movementFrom,
              toAccountId: movementTo,
              amountYen: Number(movementAmount),
              occurredAt: new Date(movementDate).toISOString(),
              description: movementDescription.trim(),
              idempotencyKey,
            },
          };
        })();
      saveAccountMovementIntent(householdId, intent);
      setPendingMovementIntent(intent);
      await recordAccountActivityMovement(intent.input);
      clearAccountMovementIntent(householdId);
      setPendingMovementIntent(null);
      onFormClean("account-movement");
      setMovementAmount("");
      setMovementDescription("");
      setMovementDate(localDateTime());
      await refresh();
      await onRecorded();
      onNoticeRef.current(
        movementKind === "TRANSFER" ? "口座移動を記録しました" : "カード請求の支払いを記録しました",
        "success",
      );
    } catch (reason) {
      if (reason instanceof MutationUserError) {
        clearAccountMovementIntent(householdId);
        setPendingMovementIntent(null);
      }
      onNoticeRef.current((reason as Error).message, "error");
    } finally {
      setPending(false);
    }
  };

  return {
    movements,
    incomes,
    movementHasNextPage,
    incomeHasNextPage,
    movementPageLoading,
    incomePageLoading,
    movementPageError,
    incomePageError,
    incomeAccountId,
    setIncomeAccountId,
    incomeAmount,
    setIncomeAmount,
    incomeDescription,
    setIncomeDescription,
    incomeDate,
    setIncomeDate,
    movementKind,
    setMovementKind,
    movementFrom,
    setMovementFrom,
    movementTo,
    setMovementTo,
    movementAmount,
    setMovementAmount,
    movementDescription,
    setMovementDescription,
    movementDate,
    setMovementDate,
    pendingIncomeIntent,
    pendingMovementIntent,
    pending,
    loadMoreMovements,
    loadMoreIncomes,
    saveIncome,
    saveMovement,
  };
}
