use async_graphql::{Context, ID, InputObject, Interface, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_application::settlement_query::GetSettlementQuery;
use bookkeeping_postgres::{
    close_period as postgres_close_period, loan_balance_query as postgres_loan_balances,
    settlement_query as postgres_settlement,
};
use chrono::{DateTime as ChronoDateTime, Utc};
use horse_postgres_common::PgPool;
use household_postgres::household_access;
use uuid::Uuid;

use crate::{DateTime, Long, MutationErrorCode, ledger_entry::LoanBalance, pagination::PageInfo};

#[derive(Clone, Debug, InputObject)]
pub struct ClosePeriodInput {
    household_id: ID,
    period: super::policy_mutations::YearMonth,
    expected_revision: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ClosePeriodSuccess {
    result: Settlement,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ClosePeriodUserError {
    pub code: MutationErrorCode,
    pub message: String,
}

#[derive(Union)]
pub enum ClosePeriodResult {
    Success(ClosePeriodSuccess),
    UserError(ClosePeriodUserError),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct OpenSettlement {
    pub(crate) id: ID,
    household_id: ID,
    period: super::policy_mutations::YearMonth,
    revision: i32,
    lines: Vec<SettlementLine>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ClosedSettlement {
    pub(crate) id: ID,
    household_id: ID,
    period: super::policy_mutations::YearMonth,
    revision: i32,
    lines: Vec<SettlementLine>,
    created_at: DateTime,
}

#[derive(Clone, Debug, Interface)]
#[graphql(
    field(name = "id", ty = "&ID"),
    field(name = "household_id", ty = "&ID"),
    field(name = "period", ty = "&super::policy_mutations::YearMonth"),
    field(name = "revision", ty = "&i32"),
    field(name = "lines", ty = "&Vec<SettlementLine>")
)]
pub enum Settlement {
    OpenSettlement(OpenSettlement),
    ClosedSettlement(ClosedSettlement),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SettlementLine {
    member_id: ID,
    member_name: String,
    scheduled_yen: Long,
    paid_yen: Long,
    carry_yen: Long,
    prior_balance_yen: Long,
    monthly_adjustment_yen: Long,
    advance_credit_yen: Long,
    amount_due_yen: Long,
    evidence: Vec<SettlementEvidence>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SettlementEvidence {
    kind: String,
    label: String,
    amount_yen: Long,
    source_id: ID,
    purchase_id: Option<ID>,
    occurred_at: Option<DateTime>,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LoanBalanceEdge {
    cursor: String,
    node: LoanBalance,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LoanBalanceConnection {
    edges: Vec<LoanBalanceEdge>,
    page_info: PageInfo,
    total_count: i32,
}

pub async fn close_period(ctx: &Context<'_>, input: ClosePeriodInput) -> Result<ClosePeriodResult> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&input.household_id)?;
    let expected_revision = match u64::try_from(input.expected_revision) {
        Ok(value) => value,
        Err(_) => return Ok(ClosePeriodResult::UserError(bad_input())),
    };
    match postgres_close_period::close_period(
        ctx.data::<PgPool>()?,
        &household_id.to_string(),
        &actor.0.to_string(),
        input.period.as_str(),
        expected_revision,
    )
    .await
    {
        Ok(_) => {
            let result =
                load_settlement(ctx.data::<PgPool>()?, household_id, input.period.as_str()).await?;
            Ok(ClosePeriodResult::Success(ClosePeriodSuccess { result }))
        }
        Err(
            postgres_close_period::ClosePeriodPersistenceError::PeriodAlreadyClosed
            | postgres_close_period::ClosePeriodPersistenceError::StalePeriod
            | postgres_close_period::ClosePeriodPersistenceError::StalePurchase,
        ) => Ok(ClosePeriodResult::UserError(user_error(
            MutationErrorCode::Conflict,
        ))),
        Err(postgres_close_period::ClosePeriodPersistenceError::DraftPurchasesExist) => {
            Ok(ClosePeriodResult::UserError(ClosePeriodUserError {
                code: MutationErrorCode::Conflict,
                message: "未確定の購入があるため締められません。".to_owned(),
            }))
        }
        Err(
            postgres_close_period::ClosePeriodPersistenceError::InvalidPeriod
            | postgres_close_period::ClosePeriodPersistenceError::InvalidDecision
            | postgres_close_period::ClosePeriodPersistenceError::InvalidUuid(_),
        ) => Ok(ClosePeriodResult::UserError(bad_input())),
        Err(
            postgres_close_period::ClosePeriodPersistenceError::HouseholdNotFound
            | postgres_close_period::ClosePeriodPersistenceError::PeriodNotFound,
        ) => Ok(ClosePeriodResult::UserError(user_error(
            MutationErrorCode::NotFound,
        ))),
        Err(error) => Err(crate::internal_error(error)),
    }
}

pub async fn settlement(
    ctx: &Context<'_>,
    household_id: ID,
    period: super::policy_mutations::YearMonth,
) -> Result<Settlement> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    ensure_member(ctx.data::<PgPool>()?, household_id, actor.0).await?;
    load_settlement(ctx.data::<PgPool>()?, household_id, period.as_str()).await
}

async fn load_settlement(pool: &PgPool, household_id: Uuid, period: &str) -> Result<Settlement> {
    let mut result = postgres_settlement::get_many(
        pool,
        &[GetSettlementQuery {
            household_id: household_id.to_string(),
            period: period.to_owned(),
        }],
    )
    .await
    .map_err(crate::internal_error)?
    .into_iter()
    .next()
    .ok_or_else(|| crate::internal_error("settlement query returned no result"))?;
    let household_node_id = encode_uuid_node("Household", household_id)?;
    let settlement_id = encode_settlement_id(household_id, period);
    let mut lines = Vec::with_capacity(result.lines.len());
    for line in result.lines.drain(..) {
        let member_id = Uuid::parse_str(&line.member_id).map_err(crate::internal_error)?;
        let evidence = result
            .evidence
            .iter()
            .filter(|evidence| evidence.member_id == line.member_id)
            .map(|evidence| {
                Ok(SettlementEvidence {
                    kind: evidence.kind.clone(),
                    label: evidence.label.clone(),
                    amount_yen: Long(evidence.amount_yen),
                    source_id: ID(evidence.source_id.clone()),
                    purchase_id: evidence
                        .purchase_id
                        .as_deref()
                        .map(|id| Uuid::parse_str(id).map_err(crate::internal_error))
                        .transpose()?
                        .map(|id| encode_uuid_node("Purchase", id))
                        .transpose()?,
                    occurred_at: evidence
                        .occurred_at_micros
                        .map(datetime_from_micros)
                        .transpose()?,
                })
            })
            .collect::<Result<Vec<_>>>()?;
        lines.push(SettlementLine {
            member_id: encode_uuid_node("Member", member_id)?,
            member_name: line.member_name,
            scheduled_yen: Long(line.scheduled_yen),
            paid_yen: Long(line.paid_yen),
            carry_yen: Long(line.carry_yen),
            prior_balance_yen: Long(line.carry_yen),
            monthly_adjustment_yen: Long(line.monthly_adjustment_yen),
            advance_credit_yen: Long(line.advance_credit_yen),
            amount_due_yen: Long(line.amount_due_yen),
            evidence,
        });
    }
    let revision = i32::try_from(result.revision).map_err(crate::internal_error)?;
    let period = super::policy_mutations::YearMonth::from_naive_date(
        chrono::NaiveDate::parse_from_str(&format!("{}-01", result.period), "%Y-%m-%d")
            .map_err(crate::internal_error)?,
    );
    match result.status.as_str() {
        "OPEN" => Ok(Settlement::OpenSettlement(OpenSettlement {
            id: ID(settlement_id),
            household_id: household_node_id.clone(),
            period,
            revision,
            lines,
        })),
        "CLOSED" => {
            let micros = result
                .created_at_micros
                .ok_or_else(|| crate::internal_error("closed settlement has no creation time"))?;
            Ok(Settlement::ClosedSettlement(ClosedSettlement {
                id: ID(settlement_id),
                household_id: household_node_id,
                period,
                revision,
                lines,
                created_at: datetime_from_micros(micros)?,
            }))
        }
        _ => Err(crate::internal_error("unknown settlement status")),
    }
}

pub async fn loan_balances(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<LoanBalanceConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    ensure_member(ctx.data::<PgPool>()?, household_id, actor.0).await?;
    let (size, backwards) = match (first, last) {
        (Some(size), None) if (1..=100).contains(&size) => (size as usize, false),
        (None, Some(size)) if (1..=100).contains(&size) => (size as usize, true),
        (None, None) => (50, false),
        _ => return Err(async_graphql::Error::new("provide either first or last")),
    };
    let rows = postgres_loan_balances::list(ctx.data::<PgPool>()?, household_id)
        .await
        .map_err(crate::internal_error)?;
    let total_count = i32::try_from(rows.len()).map_err(crate::internal_error)?;
    let after_id = decode_loan_cursor(after.as_deref())?;
    let before_id = decode_loan_cursor(before.as_deref())?;
    let after_position = after_id
        .map(|id| {
            rows.iter()
                .position(|row| row.id == id)
                .ok_or_else(|| async_graphql::Error::new("cursor not found"))
        })
        .transpose()?;
    let before_position = before_id
        .map(|id| {
            rows.iter()
                .position(|row| row.id == id)
                .ok_or_else(|| async_graphql::Error::new("cursor not found"))
        })
        .transpose()?;
    let start = after_position.map_or(0, |position| position + 1);
    let end = before_position.unwrap_or(rows.len());
    let page_rows = if start < end {
        &rows[start..end]
    } else {
        &rows[0..0]
    };
    let has_more = page_rows.len() > size;
    let page_rows = if backwards {
        &page_rows[page_rows.len().saturating_sub(size)..]
    } else {
        &page_rows[..page_rows.len().min(size)]
    };
    let has_previous_page = start > 0 || backwards && has_more;
    let has_next_page = end < rows.len() || !backwards && has_more;
    let edges = page_rows
        .iter()
        .map(|row| {
            Ok(LoanBalanceEdge {
                cursor: encode_loan_cursor(row.id),
                node: LoanBalance {
                    id: encode_uuid_node("LoanBalance", row.id)?,
                    lender_member_id: encode_uuid_node("Member", row.lender_member_id)?,
                    borrower_member_id: encode_uuid_node("Member", row.borrower_member_id)?,
                    principal_yen: Long(row.principal_yen),
                    repaid_yen: Long(row.repaid_yen),
                    balance_yen: Long(row.balance_yen),
                    created_at: row.created_at.into(),
                },
            })
        })
        .collect::<Result<Vec<_>>>()?;
    let start_cursor = edges.first().map(|edge| edge.cursor.clone());
    let end_cursor = edges.last().map(|edge| edge.cursor.clone());
    Ok(LoanBalanceConnection {
        edges,
        page_info: PageInfo {
            has_next_page,
            has_previous_page,
            start_cursor,
            end_cursor,
        },
        total_count,
    })
}

async fn ensure_member(pool: &PgPool, household_id: Uuid, actor_id: Uuid) -> Result<()> {
    let is_member = household_access::is_active_member(pool, household_id, actor_id)
        .await
        .map_err(crate::internal_error)?;
    if is_member {
        Ok(())
    } else {
        Err(async_graphql::Error::new("not found"))
    }
}

fn encode_uuid_node(kind: &str, id: Uuid) -> Result<ID> {
    let mut payload = format!("{kind}:").into_bytes();
    payload.extend_from_slice(id.as_bytes());
    Ok(ID(URL_SAFE_NO_PAD.encode(payload)))
}

fn encode_settlement_id(household_id: Uuid, period: &str) -> String {
    URL_SAFE_NO_PAD.encode(format!("Settlement:{household_id}:{period}"))
}

fn encode_loan_cursor(id: Uuid) -> String {
    URL_SAFE_NO_PAD.encode(format!("LoanBalance:{id}"))
}

fn decode_loan_cursor(cursor: Option<&str>) -> Result<Option<Uuid>> {
    let Some(cursor) = cursor else {
        return Ok(None);
    };
    if cursor.is_empty() {
        return Ok(None);
    }
    let decoded = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| async_graphql::Error::new("invalid loan cursor"))?;
    let value = std::str::from_utf8(&decoded)
        .map_err(|_| async_graphql::Error::new("invalid loan cursor"))?;
    let Some(id) = value.strip_prefix("LoanBalance:") else {
        return Err(async_graphql::Error::new("invalid loan cursor"));
    };
    Uuid::parse_str(id)
        .map(Some)
        .map_err(|_| async_graphql::Error::new("invalid loan cursor"))
}

fn datetime_from_micros(value: i64) -> Result<DateTime> {
    let instant = ChronoDateTime::<Utc>::from_timestamp_micros(value)
        .ok_or_else(|| crate::internal_error("timestamp out of range"))?;
    Ok(instant.into())
}

fn bad_input() -> ClosePeriodUserError {
    user_error(MutationErrorCode::BadInput)
}

fn user_error(code: MutationErrorCode) -> ClosePeriodUserError {
    let message = match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "この操作は利用できません。",
    };
    ClosePeriodUserError {
        code,
        message: message.to_owned(),
    }
}
