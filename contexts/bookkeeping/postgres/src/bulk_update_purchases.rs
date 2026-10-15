use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_application::bulk_update_purchases::{
    BulkUpdatePurchaseTarget, BulkUpdatePurchasesError, decide_bulk_update_purchases,
};
use bookkeeping_application::update_purchase::{UpdatePurchaseCommand, UpdatePurchaseDraft};
use chrono::{DateTime, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public, update_purchase};

#[derive(Debug, Clone)]
pub struct PersistBulkUpdatePurchases {
    pub household_id: String,
    pub actor_id: String,
}

#[derive(Debug)]
pub enum BulkUpdatePurchasesPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    HouseholdMismatch,
    MemberNotFound,
    PurchaseNotFound,
    RevisionConflict,
    AccountingPeriodClosed,
    InvalidWorkflow(BulkUpdatePurchasesError),
    InvalidUpdate(update_purchase::UpdatePurchasePersistenceError),
}

impl From<DieselError> for BulkUpdatePurchasesPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_bulk_update_purchases(
    pool: &PgPool,
    input: PersistBulkUpdatePurchases,
    commands: Vec<UpdatePurchaseCommand>,
) -> Result<Vec<String>, BulkUpdatePurchasesPersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| BulkUpdatePurchasesPersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<Vec<String>, BulkUpdatePurchasesPersistenceError, _>(async move |conn| {
            use public::accounting_periods::dsl as periods;
            use public::household_members::dsl as members;
            use public::households::dsl as households;
            use public::purchases::dsl as purchases;

            if commands.is_empty() || commands.len() > 100 {
                return Err(BulkUpdatePurchasesPersistenceError::InvalidWorkflow(
                    if commands.is_empty() {
                        BulkUpdatePurchasesError::EmptyTargets
                    } else {
                        BulkUpdatePurchasesError::TooManyTargets
                    },
                ));
            }
            let mut commands_by_id = BTreeMap::new();
            for command in &commands {
                let id = parse_uuid(&command.purchase_id, "purchase_id")?;
                if commands_by_id.insert(id, command.clone()).is_some() {
                    return Err(BulkUpdatePurchasesPersistenceError::InvalidWorkflow(
                        BulkUpdatePurchasesError::DuplicatePurchase,
                    ));
                }
            }
            let ids = commands_by_id.keys().copied().collect::<Vec<_>>();
            let observed = purchases::purchases
                .filter(purchases::id.eq_any(&ids))
                .order(purchases::id.asc())
                .select((
                    purchases::id,
                    purchases::household_id,
                    purchases::occurred_at,
                ))
                .load::<(Uuid, Uuid, DateTime<Utc>)>(conn)
                .await?;
            if observed.len() != ids.len() {
                return Err(BulkUpdatePurchasesPersistenceError::PurchaseNotFound);
            }
            if observed.iter().any(|(_, owner, _)| *owner != household_id) {
                return Err(BulkUpdatePurchasesPersistenceError::HouseholdMismatch);
            }
            let active_member = members::household_members
                .filter(members::household_id.eq(household_id))
                .filter(members::member_id.eq(actor_id))
                .filter(members::left_at.is_null())
                .select(members::member_id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !active_member {
                return Err(BulkUpdatePurchasesPersistenceError::MemberNotFound);
            }

            let mut period_starts = BTreeSet::new();
            for (id, _, occurred_at) in &observed {
                let old = update_purchase::period_start(*occurred_at)
                    .map_err(BulkUpdatePurchasesPersistenceError::InvalidUpdate)?;
                let command = &commands_by_id[id];
                let target = command
                    .occurred_at
                    .as_deref()
                    .map(update_purchase::parse_occurred_at)
                    .transpose()
                    .map_err(BulkUpdatePurchasesPersistenceError::InvalidUpdate)?
                    .unwrap_or(*occurred_at);
                period_starts.insert(old);
                period_starts.insert(
                    update_purchase::period_start(target)
                        .map_err(BulkUpdatePurchasesPersistenceError::InvalidUpdate)?,
                );
            }
            for period_start in period_starts {
                diesel::insert_into(periods::accounting_periods)
                    .values((
                        periods::household_id.eq(household_id),
                        periods::period_start.eq(period_start),
                        periods::status.eq("OPEN"),
                        periods::revision.eq(1_i64),
                    ))
                    .on_conflict((periods::household_id, periods::period_start))
                    .do_nothing()
                    .execute(&mut *conn)
                    .await?;
                let status = periods::accounting_periods
                    .filter(periods::household_id.eq(household_id))
                    .filter(periods::period_start.eq(period_start))
                    .for_update()
                    .select(periods::status)
                    .first::<String>(&mut *conn)
                    .await?;
                if status != "OPEN" {
                    return Err(BulkUpdatePurchasesPersistenceError::AccountingPeriodClosed);
                }
            }
            let household_exists = households::households
                .filter(households::id.eq(household_id))
                .for_share()
                .select(households::id)
                .first::<Uuid>(&mut *conn)
                .await
                .optional()?
                .is_some();
            if !household_exists {
                return Err(BulkUpdatePurchasesPersistenceError::HouseholdMismatch);
            }

            let locked = purchases::purchases
                .filter(purchases::id.eq_any(&ids))
                .order(purchases::id.asc())
                .for_update()
                .select((
                    purchases::id,
                    purchases::household_id,
                    purchases::revision,
                    purchases::status,
                    purchases::occurred_at,
                    purchases::date_precision,
                    purchases::merchant,
                    purchases::field_revisions,
                ))
                .load::<(
                    Uuid,
                    Uuid,
                    i64,
                    String,
                    DateTime<Utc>,
                    String,
                    String,
                    serde_json::Value,
                )>(&mut *conn)
                .await?;
            let observed_by_id = observed
                .into_iter()
                .map(|(id, owner, occurred_at)| (id, (owner, occurred_at)))
                .collect::<BTreeMap<_, _>>();
            let mut targets = Vec::with_capacity(commands.len());
            for (
                id,
                owner,
                revision,
                status,
                occurred_at,
                date_precision,
                merchant,
                field_revisions,
            ) in locked
            {
                let Some((observed_owner, observed_occurred_at)) = observed_by_id.get(&id) else {
                    return Err(BulkUpdatePurchasesPersistenceError::PurchaseNotFound);
                };
                if owner != household_id
                    || owner != *observed_owner
                    || occurred_at != *observed_occurred_at
                {
                    if owner != household_id || owner != *observed_owner {
                        return Err(BulkUpdatePurchasesPersistenceError::HouseholdMismatch);
                    }
                    return Err(BulkUpdatePurchasesPersistenceError::RevisionConflict);
                }
                let command = commands_by_id
                    .get(&id)
                    .ok_or(BulkUpdatePurchasesPersistenceError::PurchaseNotFound)?
                    .clone();
                let current = UpdatePurchaseDraft {
                    revision: u64::try_from(revision).map_err(|_| {
                        BulkUpdatePurchasesPersistenceError::InvalidUpdate(
                            update_purchase::UpdatePurchasePersistenceError::NumericOutOfRange(
                                "revision",
                            ),
                        )
                    })?,
                    status,
                    occurred_at: occurred_at.to_rfc3339(),
                    date_precision,
                    merchant,
                    field_revisions: update_purchase::json_revisions(field_revisions)
                        .map_err(BulkUpdatePurchasesPersistenceError::InvalidUpdate)?,
                };
                targets.push(BulkUpdatePurchaseTarget { command, current });
            }
            let decided_targets = targets.clone();
            let decision = decide_bulk_update_purchases(targets)
                .map_err(BulkUpdatePurchasesPersistenceError::InvalidWorkflow)?;
            let decisions = decided_targets
                .iter()
                .zip(decision.decisions)
                .map(|(target, decision)| {
                    (
                        parse_uuid(&target.command.purchase_id, "purchase_id"),
                        target.command.clone(),
                        decision,
                    )
                })
                .map(|(id, command, decision)| Ok((id?, command, decision)))
                .collect::<Result<Vec<_>, BulkUpdatePurchasesPersistenceError>>()?;
            let mut decisions_by_id = decisions
                .into_iter()
                .map(|(id, command, decision)| (id, (command, decision)))
                .collect::<BTreeMap<_, _>>();
            for id in &ids {
                let (command, decision) = decisions_by_id
                    .remove(id)
                    .ok_or(BulkUpdatePurchasesPersistenceError::PurchaseNotFound)?;
                update_purchase::persist_update_purchase_in_transaction(
                    conn,
                    household_id,
                    actor_id,
                    command,
                    decision,
                    false,
                )
                .await
                .map_err(BulkUpdatePurchasesPersistenceError::InvalidUpdate)?;
            }
            Ok(commands
                .into_iter()
                .map(|command| command.purchase_id)
                .collect())
        })
        .await
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, BulkUpdatePurchasesPersistenceError> {
    Uuid::parse_str(value).map_err(|_| BulkUpdatePurchasesPersistenceError::InvalidUuid(field))
}
