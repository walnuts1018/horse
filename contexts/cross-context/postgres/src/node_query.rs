use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, NullableExpressionMethods,
    OptionalExtension, QueryDsl,
};
use diesel_async::RunQueryDsl;
use std::collections::HashMap;
use uuid::Uuid;

use crate::{PgPool, financial_command_result, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct AccountOpeningBalanceNodeResult {
    pub account_id: Uuid,
    pub account_name: String,
    pub account_kind: String,
    pub card_last4: Option<String>,
    pub pool_id: Option<Uuid>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseCorrectionNodeResult {
    pub purchase_id: Uuid,
    pub correction: crate::purchase_corrections::PurchaseCorrectionProjection,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LedgerEntryNodeResult {
    pub id: Uuid,
    pub source_kind: String,
    pub source_id: Uuid,
    pub source_purchase_id: Option<Uuid>,
    pub occurred_at: chrono::DateTime<chrono::Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub created_at: chrono::DateTime<chrono::Utc>,
}

pub async fn load_ledger_entry_node(
    pool: &PgPool,
    household_id: Uuid,
    ledger_entry_id: Uuid,
) -> Result<Option<LedgerEntryNodeResult>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let result = public::ledger_entries::table
        .left_join(
            public::purchase_corrections::table.on(public::purchase_corrections::id
                .eq(public::ledger_entries::source_id)
                .and(
                    public::purchase_corrections::household_id
                        .eq(public::ledger_entries::household_id),
                )),
        )
        .filter(public::ledger_entries::household_id.eq(household_id))
        .filter(public::ledger_entries::id.eq(ledger_entry_id))
        .select((
            public::ledger_entries::id,
            public::ledger_entries::source_kind,
            public::ledger_entries::source_id,
            public::purchase_corrections::purchase_id.nullable(),
            public::ledger_entries::occurred_at,
            public::ledger_entries::description,
            public::ledger_entries::amount_yen,
            public::ledger_entries::created_at,
        ))
        .first::<(
            Uuid,
            String,
            Uuid,
            Option<Uuid>,
            chrono::DateTime<chrono::Utc>,
            String,
            i64,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *conn)
        .await
        .optional()?
        .map(
            |(
                id,
                source_kind,
                source_id,
                source_purchase_id,
                occurred_at,
                description,
                amount_yen,
                created_at,
            )| LedgerEntryNodeResult {
                id,
                source_kind,
                source_id,
                source_purchase_id,
                occurred_at,
                description,
                amount_yen,
                created_at,
            },
        );
    Ok(result)
}

pub async fn load_purchase_correction_node(
    pool: &PgPool,
    household_id: Uuid,
    correction_id: Uuid,
) -> Result<Option<PurchaseCorrectionNodeResult>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let Some((
        id,
        purchase_id,
        item_id,
        amount_delta_yen,
        reason,
        settlement_month,
        occurred_at,
        created_at,
    )) = public::purchase_corrections::table
        .inner_join(
            public::journals::table.on(public::journals::id
                .eq(public::purchase_corrections::journal_id)
                .and(
                    public::journals::household_id.eq(public::purchase_corrections::household_id),
                )),
        )
        .filter(public::purchase_corrections::household_id.eq(household_id))
        .filter(public::purchase_corrections::id.eq(correction_id))
        .select((
            public::purchase_corrections::id,
            public::purchase_corrections::purchase_id,
            public::purchase_corrections::item_id,
            public::purchase_corrections::amount_delta_yen,
            public::purchase_corrections::reason,
            public::purchase_corrections::effective_period_start,
            public::journals::occurred_at,
            public::purchase_corrections::created_at,
        ))
        .first::<(
            Uuid,
            Uuid,
            Option<Uuid>,
            i64,
            String,
            chrono::NaiveDate,
            chrono::DateTime<chrono::Utc>,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *conn)
        .await
        .optional()?
    else {
        return Ok(None);
    };
    let payment_allocations = public::purchase_correction_payments::table
        .inner_join(
            public::accounts::table.on(public::accounts::id
                .eq(public::purchase_correction_payments::account_id)
                .and(
                    public::accounts::household_id
                        .eq(public::purchase_correction_payments::household_id),
                )),
        )
        .filter(public::purchase_correction_payments::household_id.eq(household_id))
        .filter(public::purchase_correction_payments::correction_id.eq(id))
        .order_by(public::purchase_correction_payments::line_no)
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
            public::purchase_correction_payments::amount_yen,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>, i64)>(&mut *conn)
        .await?
        .into_iter()
        .map(
            |(
                account_id,
                account_name,
                account_kind,
                account_card_last4,
                account_pool_id,
                amount_yen,
            )| {
                crate::purchase_corrections::PurchaseCorrectionPaymentProjection {
                    account_id,
                    account_name,
                    account_kind,
                    account_card_last4,
                    account_pool_id,
                    amount_yen,
                }
            },
        )
        .collect();
    Ok(Some(PurchaseCorrectionNodeResult {
        purchase_id,
        correction: crate::purchase_corrections::PurchaseCorrectionProjection {
            id,
            item_id,
            amount_delta_yen,
            reason,
            occurred_at,
            settlement_month,
            created_at,
            payment_allocations,
        },
    }))
}

pub async fn load_account_movement_node(
    pool: &PgPool,
    household_id: Uuid,
    movement_id: Uuid,
) -> Result<Option<crate::account_history_query::AccountMovementProjection>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let Some((
        id,
        household_id,
        kind,
        occurred_at,
        from_account_id,
        to_account_id,
        amount_yen,
        description,
        created_at,
    )) = public::account_movements::table
        .filter(public::account_movements::household_id.eq(household_id))
        .filter(public::account_movements::id.eq(movement_id))
        .select((
            public::account_movements::id,
            public::account_movements::household_id,
            public::account_movements::kind,
            public::account_movements::occurred_at,
            public::account_movements::from_account_id,
            public::account_movements::to_account_id,
            public::account_movements::amount_yen,
            public::account_movements::description,
            public::account_movements::created_at,
        ))
        .first::<(
            Uuid,
            Uuid,
            String,
            chrono::DateTime<chrono::Utc>,
            Uuid,
            Uuid,
            i64,
            String,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *conn)
        .await
        .optional()?
    else {
        return Ok(None);
    };
    let accounts =
        load_account_projections(&mut conn, household_id, &[from_account_id, to_account_id])
            .await?;
    let from_account = accounts
        .iter()
        .find(|account| account.id == from_account_id)
        .cloned()
        .ok_or(NodeQueryError::InvalidPersistedData)?;
    let to_account = accounts
        .iter()
        .find(|account| account.id == to_account_id)
        .cloned()
        .ok_or(NodeQueryError::InvalidPersistedData)?;
    Ok(Some(
        crate::account_history_query::AccountMovementProjection {
            id,
            household_id,
            kind,
            occurred_at,
            from_account_id,
            from_account,
            to_account_id,
            to_account,
            amount_yen,
            description,
            created_at,
        },
    ))
}

pub async fn load_account_movement_nodes(
    pool: &PgPool,
    household_ids: &[Uuid],
    movement_ids: &[Uuid],
) -> Result<HashMap<Uuid, crate::account_history_query::AccountMovementProjection>, NodeQueryError>
{
    if household_ids.is_empty() || movement_ids.is_empty() {
        return Ok(HashMap::new());
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let rows = public::account_movements::table
        .filter(public::account_movements::household_id.eq_any(household_ids))
        .filter(public::account_movements::id.eq_any(movement_ids))
        .select((
            public::account_movements::id,
            public::account_movements::household_id,
            public::account_movements::kind,
            public::account_movements::occurred_at,
            public::account_movements::from_account_id,
            public::account_movements::to_account_id,
            public::account_movements::amount_yen,
            public::account_movements::description,
            public::account_movements::created_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            String,
            chrono::DateTime<chrono::Utc>,
            Uuid,
            Uuid,
            i64,
            String,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *conn)
        .await?;
    let account_ids = rows
        .iter()
        .flat_map(|row| [row.4, row.5])
        .collect::<Vec<_>>();
    let accounts =
        load_account_projections_for_households(&mut conn, household_ids, &account_ids).await?;
    let mut result = HashMap::with_capacity(rows.len());
    for (
        id,
        household_id,
        kind,
        occurred_at,
        from_account_id,
        to_account_id,
        amount_yen,
        description,
        created_at,
    ) in rows
    {
        let from_account = accounts
            .iter()
            .find(|account| account.id == from_account_id)
            .cloned()
            .ok_or(NodeQueryError::InvalidPersistedData)?;
        let to_account = accounts
            .iter()
            .find(|account| account.id == to_account_id)
            .cloned()
            .ok_or(NodeQueryError::InvalidPersistedData)?;
        result.insert(
            id,
            crate::account_history_query::AccountMovementProjection {
                id,
                household_id,
                kind,
                occurred_at,
                from_account_id,
                from_account,
                to_account_id,
                to_account,
                amount_yen,
                description,
                created_at,
            },
        );
    }
    Ok(result)
}

pub async fn load_account_income_node(
    pool: &PgPool,
    household_id: Uuid,
    income_id: Uuid,
) -> Result<Option<crate::account_history_query::AccountIncomeProjection>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let Some((id, household_id, account_id, amount_yen, occurred_at, description, created_at)) =
        public::account_incomes::table
            .filter(public::account_incomes::household_id.eq(household_id))
            .filter(public::account_incomes::id.eq(income_id))
            .select((
                public::account_incomes::id,
                public::account_incomes::household_id,
                public::account_incomes::account_id,
                public::account_incomes::amount_yen,
                public::account_incomes::occurred_at,
                public::account_incomes::description,
                public::account_incomes::created_at,
            ))
            .first::<(
                Uuid,
                Uuid,
                Uuid,
                i64,
                chrono::DateTime<chrono::Utc>,
                String,
                chrono::DateTime<chrono::Utc>,
            )>(&mut *conn)
            .await
            .optional()?
    else {
        return Ok(None);
    };
    let account = load_account_projections(&mut conn, household_id, &[account_id])
        .await?
        .into_iter()
        .next()
        .ok_or(NodeQueryError::InvalidPersistedData)?;
    Ok(Some(
        crate::account_history_query::AccountIncomeProjection {
            id,
            household_id,
            account_id,
            account,
            amount_yen,
            occurred_at,
            description,
            created_at,
        },
    ))
}

pub async fn load_account_income_nodes(
    pool: &PgPool,
    household_ids: &[Uuid],
    income_ids: &[Uuid],
) -> Result<HashMap<Uuid, crate::account_history_query::AccountIncomeProjection>, NodeQueryError> {
    if household_ids.is_empty() || income_ids.is_empty() {
        return Ok(HashMap::new());
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let rows = public::account_incomes::table
        .filter(public::account_incomes::household_id.eq_any(household_ids))
        .filter(public::account_incomes::id.eq_any(income_ids))
        .select((
            public::account_incomes::id,
            public::account_incomes::household_id,
            public::account_incomes::account_id,
            public::account_incomes::amount_yen,
            public::account_incomes::occurred_at,
            public::account_incomes::description,
            public::account_incomes::created_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            Uuid,
            i64,
            chrono::DateTime<chrono::Utc>,
            String,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *conn)
        .await?;
    let account_ids = rows.iter().map(|row| row.2).collect::<Vec<_>>();
    let accounts =
        load_account_projections_for_households(&mut conn, household_ids, &account_ids).await?;
    let mut result = HashMap::with_capacity(rows.len());
    for (id, household_id, account_id, amount_yen, occurred_at, description, created_at) in rows {
        let account = accounts
            .iter()
            .find(|account| account.id == account_id)
            .cloned()
            .ok_or(NodeQueryError::InvalidPersistedData)?;
        result.insert(
            id,
            crate::account_history_query::AccountIncomeProjection {
                id,
                household_id,
                account_id,
                account,
                amount_yen,
                occurred_at,
                description,
                created_at,
            },
        );
    }
    Ok(result)
}

async fn load_account_projections(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    account_ids: &[Uuid],
) -> Result<Vec<crate::account_history_query::AccountProjection>, NodeQueryError> {
    let rows = public::accounts::table
        .filter(public::accounts::household_id.eq_any(household_ids))
        .filter(public::accounts::id.eq_any(account_ids))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(conn)
        .await?;
    rows.into_iter()
        .map(|(id, name, kind, card_last4, pool_id)| {
            if !matches!(kind.as_str(), "BANK" | "CASH" | "CARD" | "OTHER") {
                return Err(NodeQueryError::InvalidPersistedData);
            }
            Ok(crate::account_history_query::AccountProjection {
                id,
                name,
                kind,
                card_last4,
                pool_id,
            })
        })
        .collect()
}

async fn load_account_projections_for_households(
    conn: &mut diesel_async::AsyncPgConnection,
    household_ids: &[Uuid],
    account_ids: &[Uuid],
) -> Result<Vec<crate::account_history_query::AccountProjection>, NodeQueryError> {
    let rows = public::accounts::table
        .filter(public::accounts::household_id.eq_any(household_ids))
        .filter(public::accounts::id.eq_any(account_ids))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(conn)
        .await?;
    rows.into_iter()
        .map(|(id, name, kind, card_last4, pool_id)| {
            if !matches!(kind.as_str(), "BANK" | "CASH" | "CARD" | "OTHER") {
                return Err(NodeQueryError::InvalidPersistedData);
            }
            Ok(crate::account_history_query::AccountProjection {
                id,
                name,
                kind,
                card_last4,
                pool_id,
            })
        })
        .collect()
}

pub async fn load_account_opening_balance_node(
    pool: &PgPool,
    household_id: Uuid,
    account_id: Uuid,
) -> Result<Option<AccountOpeningBalanceNodeResult>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let result = public::accounts::table
        .inner_join(
            public::journals::table.on(public::journals::household_id
                .eq(public::accounts::household_id)
                .and(public::journals::source_id.eq(public::accounts::id))
                .and(public::journals::source_kind.eq("ACCOUNT_OPENING_BALANCE"))),
        )
        .filter(public::accounts::household_id.eq(household_id))
        .filter(public::accounts::id.eq(account_id))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
        ))
        .first::<(Uuid, String, String, Option<String>, Option<Uuid>)>(&mut *conn)
        .await
        .optional()?
        .map(
            |(account_id, account_name, account_kind, card_last4, pool_id)| {
                AccountOpeningBalanceNodeResult {
                    account_id,
                    account_name,
                    account_kind,
                    card_last4,
                    pool_id,
                }
            },
        );
    Ok(result)
}

pub async fn load_account_opening_balance_nodes(
    pool: &PgPool,
    household_ids: &[Uuid],
    account_ids: &[Uuid],
) -> Result<HashMap<Uuid, AccountOpeningBalanceNodeResult>, NodeQueryError> {
    if household_ids.is_empty() || account_ids.is_empty() {
        return Ok(HashMap::new());
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    Ok(public::accounts::table
        .inner_join(
            public::journals::table.on(public::journals::household_id
                .eq(public::accounts::household_id)
                .and(public::journals::source_id.eq(public::accounts::id))
                .and(public::journals::source_kind.eq("ACCOUNT_OPENING_BALANCE"))),
        )
        .filter(public::accounts::household_id.eq_any(household_ids))
        .filter(public::accounts::id.eq_any(account_ids))
        .select((
            public::accounts::id,
            public::accounts::name,
            public::accounts::kind,
            public::accounts::card_last4,
            public::accounts::pool_id,
        ))
        .load::<(Uuid, String, String, Option<String>, Option<Uuid>)>(&mut *conn)
        .await?
        .into_iter()
        .map(
            |(account_id, account_name, account_kind, card_last4, pool_id)| {
                (
                    account_id,
                    AccountOpeningBalanceNodeResult {
                        account_id,
                        account_name,
                        account_kind,
                        card_last4,
                        pool_id,
                    },
                )
            },
        )
        .collect())
}

pub async fn active_household_ids(
    pool: &PgPool,
    actor_id: Uuid,
) -> Result<Vec<Uuid>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    Ok(public::household_members::table
        .filter(public::household_members::member_id.eq(actor_id))
        .filter(public::household_members::left_at.is_null())
        .order_by(public::household_members::joined_at)
        .select(public::household_members::household_id)
        .load::<Uuid>(&mut *conn)
        .await?)
}

pub async fn load_observation_node(
    pool: &PgPool,
    household_id: Uuid,
    document_id: Uuid,
    source_reference: &str,
) -> Result<Option<crate::unmatched_observations_query::ObservationRow>, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let Some((
        source_kind,
        merchant,
        occurred_at,
        amount_yen,
        order_id,
        transaction_id,
        document_created_at,
    )) = public::source_observations::table
        .inner_join(
            public::source_documents::table.on(public::source_documents::id
                .eq(public::source_observations::source_document_id)
                .and(
                    public::source_documents::household_id
                        .eq(public::source_observations::household_id),
                )),
        )
        .filter(public::source_observations::household_id.eq(household_id))
        .filter(public::source_observations::source_document_id.eq(document_id))
        .filter(public::source_observations::source_reference.eq(source_reference))
        .select((
            public::source_observations::source_kind,
            public::source_observations::merchant,
            public::source_observations::occurred_at,
            public::source_observations::amount_yen,
            public::source_observations::external_order_id,
            public::source_observations::external_transaction_id,
            public::source_documents::created_at,
        ))
        .first::<(
            String,
            String,
            Option<chrono::DateTime<chrono::Utc>>,
            Option<i64>,
            String,
            String,
            chrono::DateTime<chrono::Utc>,
        )>(&mut *conn)
        .await
        .optional()?
    else {
        return Ok(None);
    };
    let confirmation = public::source_confirmations::table
        .filter(public::source_confirmations::household_id.eq(household_id))
        .filter(public::source_confirmations::source_document_id.eq(document_id))
        .filter(public::source_confirmations::source_reference.eq(source_reference))
        .select((
            public::source_confirmations::purchase_id,
            public::source_confirmations::correction_id,
        ))
        .first::<(Option<Uuid>, Option<Uuid>)>(&mut *conn)
        .await
        .optional()?;
    let confirmed_purchase_id = match confirmation {
        Some((Some(purchase_id), _)) => Some(purchase_id),
        Some((_, Some(correction_id))) => public::purchase_corrections::table
            .filter(public::purchase_corrections::household_id.eq(household_id))
            .filter(public::purchase_corrections::id.eq(correction_id))
            .select(public::purchase_corrections::purchase_id)
            .first::<Uuid>(&mut *conn)
            .await
            .optional()?,
        _ => None,
    };
    Ok(Some(crate::unmatched_observations_query::ObservationRow {
        document_id,
        source_reference: source_reference.to_owned(),
        source_kind,
        merchant,
        occurred_at,
        amount_yen,
        order_id,
        transaction_id,
        document_created_at,
        confirmed_purchase_id,
    }))
}

pub async fn load_transfer_ledger_entry_result_by_transfer_id(
    pool: &PgPool,
    household_id: Uuid,
    transfer_id: Uuid,
) -> Result<financial_command_result::TransferLedgerEntryResult, NodeQueryError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| NodeQueryError::Pool(error.to_string()))?;
    let ledger_entry_id = public::ledger_entries::table
        .filter(public::ledger_entries::household_id.eq(household_id))
        .filter(public::ledger_entries::source_kind.eq("transfer"))
        .filter(public::ledger_entries::source_id.eq(transfer_id))
        .select(public::ledger_entries::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .ok_or(NodeQueryError::MissingLedgerEntry)?;
    drop(conn);
    financial_command_result::load_transfer_ledger_entry_result(pool, household_id, ledger_entry_id)
        .await
        .map_err(NodeQueryError::Transfer)
}

#[derive(Debug)]
pub enum NodeQueryError {
    Pool(String),
    Database(diesel::result::Error),
    MissingLedgerEntry,
    InvalidPersistedData,
    Transfer(financial_command_result::FinancialCommandResultError),
}

impl From<diesel::result::Error> for NodeQueryError {
    fn from(error: diesel::result::Error) -> Self {
        Self::Database(error)
    }
}
