import type { DocumentReview, ExtractedItem, StatementEntry } from "./imports";

type ExtractionReceipt = NonNullable<NonNullable<DocumentReview["result"]>["receipt"]>;
type ExtractionItem = NonNullable<ExtractionReceipt["items"]>[number];
type ExtractionTax = NonNullable<ExtractionReceipt["taxGroups"]>[number];
type ExtractionPayment = NonNullable<ExtractionReceipt["payments"]>[number];

export type ReceiptExtractionSnapshot = {
  jobId: string;
  nextJobId: string;
  receipt?: ExtractionReceipt;
  statements?: StatementEntry[];
};

export type ExtractionFieldChange = {
  label: string;
  before: string;
  after: string;
};

export type ExtractionRowChange = {
  id: string;
  status: "changed" | "added" | "removed" | "unmatched-current" | "unmatched-previous";
  label: string;
  changes: ExtractionFieldChange[];
};

export type ExtractionDiff = {
  receiptChanges: ExtractionFieldChange[];
  itemChanges: ExtractionRowChange[];
  taxChanges: ExtractionRowChange[];
  paymentChanges: ExtractionRowChange[];
  statementChanges: ExtractionRowChange[];
};

function value(raw: unknown): string {
  if (typeof raw === "string" || typeof raw === "number") return String(raw);
  return "未設定";
}

function yen(amount: unknown): string {
  return typeof amount !== "number" ? "未設定" : `${amount.toLocaleString("ja-JP")}円`;
}

function fields<T extends Record<string, unknown>>(
  before: T | undefined,
  after: T | undefined,
  definitions: Array<{ key: keyof T; label: string; format?: (value: unknown) => string }>,
): ExtractionFieldChange[] {
  return definitions.flatMap(({ key, label, format }) => {
    const beforeValue = before?.[key];
    const afterValue = after?.[key];
    const beforeText = format ? format(beforeValue) : value(beforeValue);
    const afterText = format ? format(afterValue) : value(afterValue);
    return beforeText === afterText ? [] : [{ label, before: beforeText, after: afterText }];
  });
}

function matchRows<T extends Record<string, unknown>>(
  before: T[] | undefined,
  after: T[] | undefined,
  identities: (entry: T) => string[],
  label: (entry: T, index: number) => string,
  definitions: Array<{ key: keyof T; label: string; format?: (value: unknown) => string }>,
  fallbackScore?: (before: T, after: T) => number,
): ExtractionRowChange[] {
  const previous = before ?? [];
  const current = after ?? [];
  const previousCounts = countIdentities(previous, identities);
  const currentCounts = countIdentities(current, identities);
  const canConfirmAdditionsAndRemovals =
    hasUniqueIdentity(previous, identities, previousCounts) &&
    hasUniqueIdentity(current, identities, currentCounts);
  const previousIndices = new Map(
    previous.flatMap((entry, index) => {
      return identities(entry).flatMap((key) =>
        previousCounts.get(key) === 1 ? [[key, index] as const] : [],
      );
    }),
  );
  const strongMatches = new Map<number, number>();
  current.forEach((entry, index) => {
    const matchingIndices = new Set(
      identities(entry)
        .filter((key) => currentCounts.get(key) === 1 && previousCounts.get(key) === 1)
        .map((key) => previousIndices.get(key))
        .filter((previousIndex): previousIndex is number => previousIndex !== undefined),
    );
    if (matchingIndices.size === 1) strongMatches.set(index, [...matchingIndices][0]);
  });
  const fallbackMatches = fallbackScore
    ? findFallbackMatches(previous, current, strongMatches, fallbackScore)
    : new Map<number, number>();
  const matchedPrevious = new Set<number>();
  const rows: ExtractionRowChange[] = [];
  current.forEach((entry, index) => {
    const previousIndex = strongMatches.get(index) ?? fallbackMatches.get(index);
    if (previousIndex === undefined || matchedPrevious.has(previousIndex)) {
      const key = identities(entry).join("|") || "unmatched";
      const status = canConfirmAdditionsAndRemovals ? "added" : "unmatched-current";
      rows.push({
        id: `${status}-${index}-${key}`,
        status,
        label: label(entry, index),
        changes: fields(undefined, entry, definitions),
      });
      return;
    }
    matchedPrevious.add(previousIndex);
    const changes = fields(previous[previousIndex], entry, definitions);
    if (!changes.length) return;
    rows.push({
      id: `changed-${previousIndex}`,
      status: "changed",
      label: label(entry, index),
      changes,
    });
  });
  previous.forEach((entry, index) => {
    if (matchedPrevious.has(index)) return;
    const key = identities(entry).join("|") || "unmatched";
    const status = canConfirmAdditionsAndRemovals ? "removed" : "unmatched-previous";
    rows.push({
      id: `${status}-${index}-${key}`,
      status,
      label: label(entry, index),
      changes: fields(entry, undefined, definitions),
    });
  });
  return rows;
}

function hasUniqueIdentity<T>(
  entries: T[],
  identities: (entry: T) => string[],
  counts: Map<string, number>,
): boolean {
  return entries.every((entry) => identities(entry).some((key) => counts.get(key) === 1));
}

function findFallbackMatches<T>(
  previous: T[],
  current: T[],
  strongMatches: Map<number, number>,
  score: (before: T, after: T) => number,
): Map<number, number> {
  const stronglyMatchedPrevious = new Set(strongMatches.values());
  const candidates = current.map((entry, currentIndex) => {
    if (strongMatches.has(currentIndex)) return [];
    return previous.flatMap((prior, previousIndex) => {
      if (stronglyMatchedPrevious.has(previousIndex)) return [];
      const matchScore = score(prior, entry);
      return matchScore > 0 ? [{ previousIndex, score: matchScore }] : [];
    });
  });
  const bestByCurrent = candidates.map((matches) => uniqueBestMatch(matches));
  const bestByPrevious = previous.map((_, previousIndex) => {
    const matches = bestByCurrent.flatMap((best, currentIndex) =>
      best?.previousIndex === previousIndex ? [{ currentIndex, score: best.score }] : [],
    );
    return uniqueBestMatch(matches);
  });
  return new Map(
    bestByCurrent.flatMap((best, currentIndex) =>
      best && bestByPrevious[best.previousIndex]?.currentIndex === currentIndex
        ? [[currentIndex, best.previousIndex] as const]
        : [],
    ),
  );
}

function uniqueBestMatch<T extends { score: number }>(matches: T[]): T | undefined {
  const highestScore = Math.max(0, ...matches.map(({ score }) => score));
  const best = matches.filter(({ score }) => score === highestScore);
  return highestScore > 0 && best.length === 1 ? best[0] : undefined;
}

function countIdentities<T>(entries: T[], identities: (entry: T) => string[]) {
  const counts = new Map<string, number>();
  entries.forEach((entry) => {
    for (const key of new Set(identities(entry))) counts.set(key, (counts.get(key) ?? 0) + 1);
  });
  return counts;
}

function itemIdentities(item: ExtractionItem): string[] {
  const identities = item.key && !/^item-\d+$/.test(item.key) ? [`key:${item.key}`] : [];
  if (!item.region) return identities;
  const { page, top, left, bottom, right } = item.region;
  identities.push(`region:${page}:${top}:${left}:${bottom}:${right}`);
  return identities;
}

function itemFallbackScore(before: ExtractionItem, after: ExtractionItem): number {
  const beforeName = before.name || before.printedName;
  const afterName = after.name || after.printedName;
  const comparable: Array<{
    before: string | number | undefined;
    after: string | number | undefined;
    primary: boolean;
  }> = [
    { before: beforeName, after: afterName, primary: true },
    { before: before.grossAmount, after: after.grossAmount, primary: true },
    { before: before.categoryHint, after: after.categoryHint, primary: false },
    { before: before.taxGroup, after: after.taxGroup, primary: false },
  ];
  const matches = comparable.filter(
    ({ before: left, after: right }) =>
      left !== undefined && right !== undefined && left !== "" && left === right,
  );
  return matches.length >= 2 && matches.some(({ primary }) => primary) ? matches.length : 0;
}

function statementIdentities(entry: StatementEntry): string[] {
  return [entry.sourceReference ? `source:${entry.sourceReference}` : `row:${entry.row}`];
}

function taxIdentities(tax: ExtractionTax): string[] {
  return tax.id ? [`tax:${tax.id}`] : [];
}

function paymentIdentities(payment: ExtractionPayment): string[] {
  if (!payment.method && !payment.cardLast4) return [];
  return [
    ...(payment.cardLast4 ? [`card:${payment.cardLast4}`] : []),
    `payment:${JSON.stringify([payment.method, payment.cardLast4 ?? ""])}`,
  ];
}

function statementKind(kind: string): string {
  const labels: Record<string, string> = {
    ACCOUNT_TRANSFER: "口座振替",
    CARD_BILL_PAYMENT: "カード引落し",
    CARD_PURCHASE: "カード利用",
    EXTERNAL_INCOME: "入金",
    REFUND: "返金",
  };
  return labels[kind] ?? kind;
}

function statementDate(raw: unknown): string {
  if (typeof raw !== "string") return "未設定";
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return raw;
  return new Intl.DateTimeFormat("ja-JP", {
    timeZone: "Asia/Tokyo",
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

const yenField = (amount: unknown) => yen(amount);

function emptyDiff(): ExtractionDiff {
  return {
    receiptChanges: [],
    itemChanges: [],
    taxChanges: [],
    paymentChanges: [],
    statementChanges: [],
  };
}

export function compareExtractions(
  before: Pick<ReceiptExtractionSnapshot, "receipt" | "statements">,
  after: Pick<ReceiptExtractionSnapshot, "receipt" | "statements">,
): ExtractionDiff {
  if (!before.receipt && !before.statements && !after.receipt && !after.statements) {
    return emptyDiff();
  }
  const previousReceipt = before.receipt;
  const currentReceipt = after.receipt;
  const itemDefinitions = [
    { key: "name", label: "品目名" },
    { key: "grossAmount", label: "金額", format: yenField },
    { key: "categoryHint", label: "カテゴリ" },
    { key: "taxGroup", label: "税区分" },
  ] satisfies Array<{
    key: keyof ExtractedItem;
    label: string;
    format?: (value: unknown) => string;
  }>;
  const taxDefinitions = [
    { key: "numerator", label: "税率分子" },
    { key: "denominator", label: "税率分母" },
    { key: "basis", label: "計算対象" },
    { key: "rounding", label: "端数処理" },
    { key: "printedSubtotal", label: "印字小計", format: yenField },
    { key: "printedTax", label: "印字税額", format: yenField },
  ] satisfies Array<{
    key: keyof ExtractionTax;
    label: string;
    format?: (value: unknown) => string;
  }>;
  const paymentDefinitions = [
    { key: "method", label: "支払方法" },
    { key: "cardLast4", label: "カード番号下4桁" },
    { key: "amount", label: "支払額", format: yenField },
  ] satisfies Array<{
    key: keyof ExtractionPayment;
    label: string;
    format?: (value: unknown) => string;
  }>;
  const statementDefinitions = [
    { key: "merchant", label: "加盟店" },
    { key: "amount", label: "金額", format: yenField },
    { key: "occurredAt", label: "日時", format: statementDate },
    { key: "kind", label: "種別", format: (kind) => statementKind(value(kind)) },
    { key: "transactionId", label: "取引ID" },
    { key: "orderId", label: "注文ID" },
    { key: "cardLast4", label: "カード番号下4桁" },
  ] satisfies Array<{
    key: keyof StatementEntry;
    label: string;
    format?: (value: unknown) => string;
  }>;

  return {
    receiptChanges: fields(
      previousReceipt ? { total: previousReceipt.total } : undefined,
      currentReceipt ? { total: currentReceipt.total } : undefined,
      [{ key: "total", label: "合計", format: yenField }],
    ),
    itemChanges: matchRows(
      previousReceipt?.items,
      currentReceipt?.items,
      itemIdentities,
      (item, index) => `明細${index + 1}・${item.name || item.printedName || "品目"}`,
      itemDefinitions,
      itemFallbackScore,
    ),
    taxChanges: matchRows(
      previousReceipt?.taxGroups,
      currentReceipt?.taxGroups,
      taxIdentities,
      (tax, index) => `税${index + 1}・${tax.id || "区分"}`,
      taxDefinitions,
    ),
    paymentChanges: matchRows(
      previousReceipt?.payments,
      currentReceipt?.payments,
      paymentIdentities,
      (payment, index) => `支払${index + 1}・${payment.method || "方法"}`,
      paymentDefinitions,
    ),
    statementChanges: matchRows(
      before.statements,
      after.statements,
      statementIdentities,
      (entry) => `取引明細 行${entry.row}・${entry.merchant || statementKind(entry.kind)}`,
      statementDefinitions,
    ),
  };
}

export function readReceiptExtractionSnapshot(
  documentId: string,
): ReceiptExtractionSnapshot | null {
  try {
    const stored: unknown = JSON.parse(
      sessionStorage.getItem(`document-extraction:${documentId}`) ?? "null",
    );
    return isReceiptExtractionSnapshot(stored) ? stored : null;
  } catch {
    return null;
  }
}

function isReceiptExtractionSnapshot(raw: unknown): raw is ReceiptExtractionSnapshot {
  if (typeof raw !== "object" || raw === null) return false;
  if (!("jobId" in raw) || typeof raw.jobId !== "string") return false;
  if (!("nextJobId" in raw) || typeof raw.nextJobId !== "string") return false;
  const receipt = "receipt" in raw && typeof raw.receipt === "object" && raw.receipt !== null;
  const statements = "statements" in raw && Array.isArray(raw.statements);
  return receipt || statements;
}

export function storeReceiptExtractionSnapshot(
  documentId: string,
  snapshot: ReceiptExtractionSnapshot,
) {
  try {
    sessionStorage.setItem(`document-extraction:${documentId}`, JSON.stringify(snapshot));
  } catch {
    return;
  }
}
