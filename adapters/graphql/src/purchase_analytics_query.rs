use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::purchase_analytics_query::{
    PurchaseAnalyticsQuery, PurchaseAnalyticsQueryError,
};
use chrono::NaiveDate;
use uuid::Uuid;

use crate::{Long, policy_mutations::YearMonth, scalars::DateTime};

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchaseAnalyticsEntry {
    pub dimension: String,
    pub id: String,
    pub name: String,
    pub period: YearMonth,
    pub amount_yen: Long,
    pub count: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PurchasePriceObservation {
    pub item_id: String,
    pub occurred_at: DateTime,
    pub amount_yen: Long,
    pub remaining_refundable_amount_yen: Long,
    pub effective_unit_price_numerator_yen: Option<String>,
    pub effective_unit_price_denominator: Option<String>,
    pub printed_unit_price_yen: Option<Long>,
    pub printed_unit_price_quantity_numerator: Option<String>,
    pub printed_unit_price_quantity_denominator: Option<String>,
    pub printed_unit_price_unit: Option<String>,
}

pub async fn graphql_purchase_analytics(
    ctx: &Context<'_>,
    household_id: ID,
    from: YearMonth,
    to: YearMonth,
) -> Result<Vec<PurchaseAnalyticsEntry>> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let rows = ctx
        .data::<std::sync::Arc<dyn PurchaseAnalyticsQuery>>()?
        .purchase_analytics(actor.0, household_id, from.as_str(), to.as_str())
        .await
        .map_err(map_query_error)?;
    rows.into_iter()
        .map(|row| {
            let id = encode_analytics_id(&row.dimension, &row.id)?;
            let period = NaiveDate::parse_from_str(&format!("{}-01", row.period), "%Y-%m-%d")
                .map_err(crate::internal_error)?;
            let count = i32::try_from(row.count).map_err(crate::internal_error)?;
            Ok(PurchaseAnalyticsEntry {
                dimension: row.dimension,
                id,
                name: row.name,
                period: YearMonth::from_naive_date(period),
                amount_yen: Long(row.amount_yen),
                count,
            })
        })
        .collect()
}

pub async fn graphql_purchase_price_observations(
    ctx: &Context<'_>,
    household_id: ID,
    from: YearMonth,
    to: YearMonth,
    item_ids: Vec<String>,
) -> Result<Vec<PurchasePriceObservation>> {
    if item_ids.len() > 5 {
        return Err(Error::new("at most five item IDs may be requested"));
    }
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let db_item_ids = item_ids
        .iter()
        .map(|value| decode_analytics_item_id(value).ok_or_else(|| Error::new("invalid item ID")))
        .collect::<Result<Vec<_>>>()?;
    let rows = ctx
        .data::<std::sync::Arc<dyn PurchaseAnalyticsQuery>>()?
        .purchase_price_observations(
            actor.0,
            household_id,
            from.as_str(),
            to.as_str(),
            &db_item_ids,
        )
        .await
        .map_err(map_query_error)?;
    rows.into_iter()
        .map(|row| {
            Ok(PurchasePriceObservation {
                item_id: encode_analytics_id("item", &row.item_id)?,
                occurred_at: row.occurred_at.into(),
                amount_yen: Long(row.amount_yen),
                remaining_refundable_amount_yen: Long(row.remaining_refundable_amount_yen),
                effective_unit_price_numerator_yen: row.effective_unit_price_numerator_yen,
                effective_unit_price_denominator: row.effective_unit_price_denominator,
                printed_unit_price_yen: row.printed_unit_price_yen.map(Long),
                printed_unit_price_quantity_numerator: row.printed_unit_price_quantity_numerator,
                printed_unit_price_quantity_denominator: row
                    .printed_unit_price_quantity_denominator,
                printed_unit_price_unit: row.printed_unit_price_unit,
            })
        })
        .collect()
}

fn encode_analytics_id(dimension: &str, raw: &str) -> Result<String> {
    if raw == "__correction__" {
        return Ok(raw.to_owned());
    }
    match dimension {
        "pool" => encode_node_id("Pool", raw),
        "account" => encode_node_id("Account", raw),
        "item" => {
            let (base, unit) = raw.split_once('\u{1f}').unwrap_or((raw, ""));
            let base = match Uuid::parse_str(base) {
                Ok(id) => encode_node_id("ProductVariant", &id.to_string())?,
                Err(_) => base.to_owned(),
            };
            Ok(format!("{base}\0{unit}"))
        }
        _ => Ok(raw.to_owned()),
    }
}

fn decode_analytics_item_id(value: &str) -> Option<String> {
    let (base, unit) = value.split_once('\0')?;
    let base = decode_node_id(base, "ProductVariant")
        .map(|id| id.to_string())
        .unwrap_or_else(|| base.to_owned());
    Some(format!("{base}\u{1f}{unit}"))
}

fn encode_node_id(kind: &str, raw: &str) -> Result<String> {
    let id = Uuid::parse_str(raw).map_err(crate::internal_error)?;
    if id.is_nil() {
        return Err(crate::internal_error("invalid analytics node ID"));
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(URL_SAFE_NO_PAD.encode(payload))
}

fn decode_node_id(value: &str, expected_kind: &str) -> Option<Uuid> {
    let bytes = URL_SAFE_NO_PAD.decode(value).ok()?;
    let separator = bytes.iter().position(|byte| *byte == b':')?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return None;
    }
    let id = Uuid::from_slice(payload).ok()?;
    (!id.is_nil()).then_some(id)
}

fn map_query_error(error: PurchaseAnalyticsQueryError) -> Error {
    use PurchaseAnalyticsQueryError as QueryError;
    match error {
        QueryError::HouseholdNotFound => Error::new("household access denied"),
        QueryError::InvalidPeriod => Error::new("invalid purchase analytics period"),
        QueryError::AmountOutOfRange => {
            Error::new("purchase analytics total exceeds supported range")
        }
        QueryError::Infrastructure(_) => crate::internal_error(error),
    }
}
