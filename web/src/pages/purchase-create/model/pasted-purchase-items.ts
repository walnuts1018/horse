export type ParsedPurchaseItemPaste = {
  items: { name: string; amount: string }[];
  invalidRowCount: number;
  invalidSyntax: boolean;
  structured: boolean;
};

const amountPattern = /^¥?\d+(?:,\d{3})*$/;

export function parsePastedPurchaseItems(text: string): ParsedPurchaseItemPaste {
  const delimiter = text.includes("\t") ? "\t" : ",";
  const hasRows = text.includes("\n") || text.includes("\r") || text.includes("\t");
  if (!hasRows && !text.includes(","))
    return { items: [], invalidRowCount: 0, invalidSyntax: false, structured: false };

  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  let closedQuote = false;
  let invalidSyntax = false;

  for (let index = 0; index < text.length; index += 1) {
    const character = text[index];
    if (quoted) {
      if (character === '"') {
        if (text[index + 1] === '"') {
          field += '"';
          index += 1;
        } else {
          quoted = false;
          closedQuote = true;
        }
      } else {
        field += character;
      }
      continue;
    }

    if (character === '"') {
      if (field.trim() !== "" || closedQuote) invalidSyntax = true;
      else quoted = true;
    } else if (character === delimiter) {
      row.push(field.trim());
      field = "";
      closedQuote = false;
    } else if (character === "\n" || character === "\r") {
      if (character === "\r" && text[index + 1] === "\n") index += 1;
      row.push(field.trim());
      rows.push(row);
      row = [];
      field = "";
      closedQuote = false;
    } else if (closedQuote) {
      if (!/\s/.test(character)) invalidSyntax = true;
      else field += character;
    } else {
      field += character;
    }
  }

  if (quoted) invalidSyntax = true;
  row.push(field.trim());
  rows.push(row);

  const items: ParsedPurchaseItemPaste["items"] = [];
  let invalidRowCount = 0;
  let hasValidCSVRow = false;
  for (const cells of rows) {
    if (cells.length === 1 && cells[0] === "") continue;
    const name = cells[0]?.trim() ?? "";
    const amount = cells[1]?.trim() ?? "";
    if (cells.length === 2 && name && amountPattern.test(amount)) hasValidCSVRow = true;
    if (cells.length !== 2 || !name || !amountPattern.test(amount)) {
      invalidRowCount += 1;
      continue;
    }
    items.push({ name, amount: amount.replace(/[¥,]/g, "") });
  }

  return { items, invalidRowCount, invalidSyntax, structured: hasRows || hasValidCSVRow };
}
