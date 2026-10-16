use chrono::{DateTime, FixedOffset, NaiveDate, NaiveDateTime};
use csv::{ReaderBuilder, StringRecord};
use serde::Serialize;

const MAX_DOCUMENT_BYTES: usize = 20 << 20;
const MAX_STATEMENTS: usize = 10_000;
const MAX_AMOUNT: i64 = 1_000_000_000_000;

#[derive(Serialize, Clone)]
#[serde(rename_all = "camelCase")]
pub struct StatementEntry {
    pub row: usize,
    pub source_reference: String,
    pub kind: String,
    pub occurred_at: String,
    pub merchant: String,
    pub amount: i64,
    pub transaction_id: String,
    pub order_id: String,
    pub card_last4: String,
}

pub struct VerifiedStatements {
    pub entries: Vec<StatementEntry>,
    pub warnings: Vec<String>,
}

pub fn parse_and_verify(
    data: &[u8],
    source_kind: &str,
) -> Result<VerifiedStatements, &'static str> {
    if data.is_empty() || data.len() > MAX_DOCUMENT_BYTES || !matches!(source_kind, "card" | "bank")
    {
        return Err("invalid_statement_source");
    }
    let data = data.strip_prefix(&[0xef, 0xbb, 0xbf]).unwrap_or(data);
    let mut reader = ReaderBuilder::new().flexible(false).from_reader(data);
    let header = reader
        .headers()
        .map_err(|_| "invalid_statement_header")?
        .clone();
    let columns = parse_header(&header)?;
    for required in ["date", "merchant", "amount"] {
        if !columns.contains_key(required) {
            return Err("missing_statement_column");
        }
    }
    let mut entries = Vec::new();
    let mut warnings = Vec::new();
    for (index, record) in reader.records().enumerate() {
        let row_number = index + 2;
        if row_number > MAX_STATEMENTS + 1 {
            return Err("statement_too_many_rows");
        }
        let record = record.map_err(|_| "invalid_statement_row")?;
        let get = |key: &str| -> &str {
            columns
                .get(key)
                .and_then(|column| record.get(*column))
                .unwrap_or_default()
                .trim()
        };
        let occurred_at = parse_date(get("date")).ok_or("invalid_statement_date")?;
        let amount = parse_amount(get("amount")).ok_or("invalid_statement_amount")?;
        let merchant = get("merchant").to_owned();
        if merchant.is_empty() || merchant.len() > 1024 {
            return Err("invalid_statement_merchant");
        }
        let kind = if source_kind == "card" {
            "CARD_PURCHASE".to_owned()
        } else {
            bank_transaction_kind(get("transactionKind"))
        };
        if source_kind == "bank" && kind == "UNCLASSIFIED" {
            warnings.push(format!("取引{}の種別が未確定です。", row_number));
        }
        let transaction_id = get("transactionId").to_owned();
        let order_id = get("orderId").to_owned();
        let card_last4 = get("cardLast4").to_owned();
        if transaction_id.len() > 256
            || order_id.len() > 256
            || !card_last4.is_empty()
                && (card_last4.len() != 4 || !card_last4.bytes().all(|byte| byte.is_ascii_digit()))
            || !valid_statement_kind(source_kind, &kind)
            || !valid_statement_amount(&kind, amount)
        {
            return Err("invalid_statement_row");
        }
        entries.push(StatementEntry {
            row: row_number,
            source_reference: format!("row:{row_number}"),
            kind,
            occurred_at,
            merchant,
            amount,
            transaction_id,
            order_id,
            card_last4,
        });
    }
    if entries.is_empty() {
        return Err("statement_has_no_transactions");
    }
    Ok(VerifiedStatements { entries, warnings })
}

fn parse_header(
    header: &StringRecord,
) -> Result<std::collections::HashMap<&'static str, usize>, &'static str> {
    let mut columns = std::collections::HashMap::new();
    for (index, name) in header.iter().enumerate() {
        let Some(key) = header_key(name) else {
            continue;
        };
        if columns.insert(key, index).is_some() {
            return Err("duplicate_statement_column");
        }
    }
    Ok(columns)
}

fn header_key(value: &str) -> Option<&'static str> {
    match value.trim().to_lowercase().as_str() {
        "date" | "日時" | "日付" | "利用日" | "ご利用日" | "取引日" => Some("date"),
        "merchant" | "店名" | "店舗" | "利用店名" | "ご利用店名" | "摘要" => {
            Some("merchant")
        }
        "amount" | "金額" | "利用金額" | "ご利用金額" | "取引金額" => Some("amount"),
        "transactionid" | "transaction_id" | "取引id" | "取引番号" => Some("transactionId"),
        "orderid" | "order_id" | "注文番号" => Some("orderId"),
        "cardlast4" | "card_last4" | "カード末尾" => Some("cardLast4"),
        "kind" | "type" | "transactionkind" | "transaction_kind" | "取引種別" | "取引区分"
        | "種別" => Some("transactionKind"),
        _ => None,
    }
}

fn parse_date(value: &str) -> Option<String> {
    if let Ok(date) = DateTime::parse_from_rfc3339(value) {
        return Some(date.to_rfc3339());
    }
    let jst = FixedOffset::east_opt(9 * 60 * 60)?;
    for format in ["%Y-%m-%d %H:%M:%S", "%Y/%m/%d %H:%M"] {
        if let Ok(date) = NaiveDateTime::parse_from_str(value, format) {
            return Some(date.and_local_timezone(jst).single()?.to_rfc3339());
        }
    }
    for format in ["%Y-%m-%d", "%Y/%m/%d", "%Y/%-m/%-d", "%Y年%-m月%-d日"] {
        if let Ok(date) = NaiveDate::parse_from_str(value, format) {
            return Some(
                date.and_hms_opt(0, 0, 0)?
                    .and_local_timezone(jst)
                    .single()?
                    .to_rfc3339(),
            );
        }
    }
    None
}

fn parse_amount(value: &str) -> Option<i64> {
    let value = value.trim().replace(',', "");
    let value = value
        .strip_prefix('¥')
        .or_else(|| value.strip_prefix('￥'))
        .unwrap_or(&value)
        .trim();
    let value = value.strip_suffix('円').unwrap_or(value).trim();
    let value = if value.starts_with('(') && value.ends_with(')') {
        format!("-{}", value.trim_start_matches('(').trim_end_matches(')'))
    } else {
        value.to_owned()
    };
    let amount = value.parse::<i64>().ok()?;
    (amount.unsigned_abs() <= MAX_AMOUNT as u64).then_some(amount)
}

fn bank_transaction_kind(value: &str) -> String {
    match value.trim().to_uppercase().as_str() {
        "PURCHASE_PAYMENT" | "商品購入" | "デビット購入" => "PURCHASE_PAYMENT".to_owned(),
        "CARD_BILL_PAYMENT" | "CARD_PAYMENT" | "クレジットカード引落" | "カード利用代金" => {
            "CARD_BILL_PAYMENT".to_owned()
        }
        "EXTERNAL_INCOME" | "INCOME" | "給与" | "賞与" | "利息" | "還付金" | "配当金" => {
            "EXTERNAL_INCOME".to_owned()
        }
        "ACCOUNT_TRANSFER" | "TRANSFER" | "振込" | "振替" => "ACCOUNT_TRANSFER".to_owned(),
        "LOAN" | "貸付" | "貸付金" | "借入" | "借入金" => "LOAN".to_owned(),
        "REPAYMENT" | "貸付返済" | "借入返済" | "ローン返済" | "返済" => {
            "REPAYMENT".to_owned()
        }
        "REFUND" | "返金" | "払い戻し" => "REFUND".to_owned(),
        _ => "UNCLASSIFIED".to_owned(),
    }
}

fn valid_statement_amount(kind: &str, amount: i64) -> bool {
    amount != 0
        && match kind {
            "EXTERNAL_INCOME" => amount > 0,
            "REFUND" | "CARD_BILL_PAYMENT" => amount < 0,
            _ => true,
        }
}

fn valid_statement_kind(source_kind: &str, kind: &str) -> bool {
    if source_kind == "card" {
        matches!(kind, "CARD_PURCHASE" | "REFUND")
    } else {
        matches!(
            kind,
            "PURCHASE_PAYMENT"
                | "CARD_BILL_PAYMENT"
                | "ACCOUNT_TRANSFER"
                | "LOAN"
                | "REPAYMENT"
                | "EXTERNAL_INCOME"
                | "REFUND"
                | "UNCLASSIFIED"
        )
    }
}

#[cfg(test)]
mod tests {
    use super::parse_and_verify;

    #[test]
    fn transaction_amount_signs_follow_statement_kind() {
        let refund = b"date,merchant,amount,transactionKind\n2026-01-01,Shop,-100,REFUND\n";
        assert!(parse_and_verify(refund, "bank").is_ok());

        let invalid_refund = b"date,merchant,amount,transactionKind\n2026-01-01,Shop,100,REFUND\n";
        assert!(matches!(
            parse_and_verify(invalid_refund, "bank"),
            Err("invalid_statement_row")
        ));

        let invalid_zero = b"date,merchant,amount,transactionKind\n2026-01-01,Shop,0,TRANSFER\n";
        assert!(matches!(
            parse_and_verify(invalid_zero, "bank"),
            Err("invalid_statement_row")
        ));
    }
}
