const amountFormatter = new Intl.NumberFormat("ja-JP", {
  maximumFractionDigits: 0,
  useGrouping: true,
});
const groupingSample = amountFormatter.formatToParts(123456789012345678901234567890n);
const groupSeparator = groupingSample.find((part) => part.type === "group")?.value ?? ",";
const integerGroupLengths = groupingSample
  .filter((part) => part.type === "integer")
  .map((part) => part.value.length);
const primaryGroupLength = integerGroupLengths.at(-1) ?? 3;
const secondaryGroupLength = integerGroupLengths.at(-2) ?? primaryGroupLength;

export function formatAmountInput(value: string): string {
  const digits = value.replace(/\D/g, "");
  if (!digits) return "";
  if (digits.length <= primaryGroupLength) return digits;

  const firstGroupLength =
    (digits.length - primaryGroupLength) % secondaryGroupLength || secondaryGroupLength;
  const groups = [digits.slice(0, firstGroupLength)];
  for (let offset = firstGroupLength; offset < digits.length; offset += secondaryGroupLength) {
    groups.push(digits.slice(offset, offset + secondaryGroupLength));
  }
  return groups.join(groupSeparator);
}

export function amountInputCaret(value: string, caret: number): number {
  const digitCount = value.slice(0, caret).replace(/\D/g, "").length;
  if (digitCount === 0) return 0;
  const formatted = formatAmountInput(value);
  let seen = 0;
  for (let index = 0; index < formatted.length; index += 1) {
    if (/\d/.test(formatted[index])) seen += 1;
    if (seen === digitCount) {
      let position = index + 1;
      while (formatted.startsWith(groupSeparator, position)) position += groupSeparator.length;
      return position;
    }
  }
  return formatted.length;
}
