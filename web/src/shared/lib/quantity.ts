const maxNumerator = 1_000_000_000_000n;
const maxDenominator = 1_000_000n;

export type Quantity = { numerator: string; denominator: string; unit: string };

const gcd = (left: bigint, right: bigint) => {
  let a = left;
  let b = right;
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
};

export function parseQuantity(value: string, unitValue: string): Quantity | null {
  const raw = value.trim();
  const unit = unitValue.trim();
  if (!raw && !unit) return null;
  if (!raw || raw.length > 64 || !unit || new TextEncoder().encode(unit).length > 32)
    throw new Error("数量と単位を確認してください");

  let numerator: bigint;
  let denominator: bigint;
  const fraction = /^(\d+)\s*\/\s*(\d+)$/.exec(raw);
  if (fraction) {
    numerator = BigInt(fraction[1]);
    denominator = BigInt(fraction[2]);
  } else {
    const decimal = /^(\d+)(?:\.(\d+))?$/.exec(raw);
    if (!decimal) throw new Error("数量は整数、小数、分数で入力してください");
    const places = decimal[2]?.length ?? 0;
    denominator = 10n ** BigInt(places);
    numerator = BigInt(decimal[1]) * denominator + BigInt(decimal[2] ?? "0");
  }
  if (numerator <= 0n || denominator <= 0n) throw new Error("数量は0より大きい値にしてください");
  const divisor = gcd(numerator, denominator);
  numerator /= divisor;
  denominator /= divisor;
  if (numerator > maxNumerator || denominator > maxDenominator)
    throw new Error("数量の範囲を確認してください");
  return { numerator: numerator.toString(), denominator: denominator.toString(), unit };
}

export function formatQuantity(numerator?: string | null, denominator?: string | null) {
  if (!numerator || !denominator) return "";
  return denominator === "1" ? numerator : `${numerator}/${denominator}`;
}
