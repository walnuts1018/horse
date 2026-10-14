import type { Account } from "../../../shared/api";

function normalizeName(name: string) {
  return name.trim().replace(/\s+/g, " ").toLocaleLowerCase("ja-JP");
}

export function receiptPaymentAccountId(
  accounts: Account[],
  method: string,
  cardLast4?: string,
): string {
  if (cardLast4 && /^\d{4}$/.test(cardLast4)) {
    const matches = accounts.filter(
      (account) => account.kind === "CARD" && account.cardLast4 === cardLast4,
    );
    if (matches.length === 1) return matches[0].id;
  }

  const normalizedMethod = normalizeName(method);
  const methodMatches = accounts.filter(
    (account) =>
      normalizeName(account.name) === normalizedMethod ||
      normalizeName(account.kind) === normalizedMethod,
  );
  return methodMatches.length === 1 ? methodMatches[0].id : "";
}
