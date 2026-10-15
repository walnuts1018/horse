use std::collections::BTreeMap;

use bookkeeping_workflows::update_purchase::{
    UpdatePurchaseDecision, UpdatePurchaseDraft, UpdatePurchaseError, decide_update_purchase,
};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

#[derive(Debug, Clone)]
pub struct PersistUpdatePurchase {
    pub household_id: String,
    pub actor_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedUpdatePurchase {
    pub purchase_id: String,
    pub revision: u64,
}

#[derive(Debug)]
pub enum UpdatePurchasePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidOccurredAt,
    NumericOutOfRange(&'static str),
    HouseholdMismatch,
    MemberNotFound,
    PurchaseNotFound,
    AccountingPeriodClosed,
    AccountNotFound,
    InvalidDecision(UpdatePurchaseError),
}

impl From<DieselError> for UpdatePurchasePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_update_purchase(
    pool: &PgPool,
    input: PersistUpdatePurchase,
    command: bookkeeping_workflows::update_purchase::UpdatePurchaseCommand,
    decision: UpdatePurchaseDecision,
) -> Result<PersistedUpdatePurchase, UpdatePurchasePersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let purchase_id = parse_uuid(&command.purchase_id, "purchase_id")?;
    let revision = i64::try_from(decision.revision)
        .map_err(|_| UpdatePurchasePersistenceError::NumericOutOfRange("revision"))?;
    let changed_fields_json = serde_json::Value::Object(
        decision
            .field_revisions
            .iter()
            .map(|(field, revision)| {
                let value = i64::try_from(*revision)
                    .map(serde_json::Value::from)
                    .map_err(|_| {
                        UpdatePurchasePersistenceError::NumericOutOfRange("field_revision")
                    })?;
                Ok((field.clone(), value))
            })
            .collect::<Result<serde_json::Map<_, _>, UpdatePurchasePersistenceError>>()?,
    );
    let occurred_at = decision
        .occurred_at
        .as_deref()
        .map(parse_occurred_at)
        .transpose()?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| UpdatePurchasePersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedUpdatePurchase, UpdatePurchasePersistenceError, _>(
            async move |conn| {
                use public::accounting_periods::dsl as periods;
                use public::household_events::dsl as events;
                use public::households::dsl as households;
                use public::purchase_payments::dsl as payments;
                use public::purchases::dsl as purchases;

                let observed = purchases::purchases
                    .filter(purchases::id.eq(purchase_id))
                    .select((purchases::household_id, purchases::occurred_at))
                    .first::<(Uuid, DateTime<Utc>)>(conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchasePersistenceError::PurchaseNotFound)?;
                if observed.0 != household_id {
                    return Err(UpdatePurchasePersistenceError::HouseholdMismatch);
                }
                require_member(&mut *conn, household_id, actor_id).await?;

                let target_occurred_at = occurred_at.unwrap_or(observed.1);
                let old_period = period_start(observed.1)?;
                let target_period = period_start(target_occurred_at)?;
                let mut lock_periods = vec![old_period, target_period];
                lock_periods.sort();
                lock_periods.dedup();
                for period_start in lock_periods {
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
                        return Err(UpdatePurchasePersistenceError::AccountingPeriodClosed);
                    }
                }

                let locked = purchases::purchases
                    .filter(purchases::id.eq(purchase_id))
                    .for_update()
                    .select((
                        purchases::household_id,
                        purchases::revision,
                        purchases::status,
                        purchases::occurred_at,
                        purchases::date_precision,
                        purchases::merchant,
                        purchases::field_revisions,
                        purchases::payment_account_id,
                    ))
                    .first::<(
                        Uuid,
                        i64,
                        String,
                        DateTime<Utc>,
                        String,
                        String,
                        serde_json::Value,
                        Option<Uuid>,
                    )>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(UpdatePurchasePersistenceError::PurchaseNotFound)?;
                if locked.0 != household_id || period_start(locked.3)? != old_period {
                    return Err(UpdatePurchasePersistenceError::HouseholdMismatch);
                }
                require_member(&mut *conn, household_id, actor_id).await?;
                let household_exists = households::households
                    .filter(households::id.eq(household_id))
                    .for_share()
                    .select(households::id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .is_some();
                if !household_exists {
                    return Err(UpdatePurchasePersistenceError::HouseholdMismatch);
                }

                let field_revisions = json_revisions(locked.6)?;
                let current = UpdatePurchaseDraft {
                    revision: u64::try_from(locked.1).map_err(|_| {
                        UpdatePurchasePersistenceError::NumericOutOfRange("revision")
                    })?,
                    status: locked.2,
                    occurred_at: locked.3.to_rfc3339(),
                    date_precision: locked.4.clone(),
                    merchant: locked.5.clone(),
                    field_revisions,
                };
                let decided = decide_update_purchase(command.clone(), current)
                    .map_err(UpdatePurchasePersistenceError::InvalidDecision)?;
                if decided != decision {
                    return Err(UpdatePurchasePersistenceError::InvalidDecision(
                        UpdatePurchaseError::Conflict,
                    ));
                }

                if let Some(payment_list) = &decision.payment_allocations {
                    for payment in payment_list {
                        let account_id = parse_uuid(&payment.account_id, "payment_account_id")?;
                        let belongs = public::accounts::dsl::accounts
                            .filter(public::accounts::dsl::id.eq(account_id))
                            .filter(public::accounts::dsl::household_id.eq(household_id))
                            .select(public::accounts::dsl::id)
                            .first::<Uuid>(&mut *conn)
                            .await
                            .optional()?
                            .is_some();
                        if !belongs {
                            return Err(UpdatePurchasePersistenceError::AccountNotFound);
                        }
                    }
                    diesel::delete(
                        payments::purchase_payments
                            .filter(payments::household_id.eq(household_id))
                            .filter(payments::purchase_id.eq(purchase_id)),
                    )
                    .execute(&mut *conn)
                    .await?;
                    for (index, payment) in payment_list.iter().enumerate() {
                        let line_no = i32::try_from(index + 1).map_err(|_| {
                            UpdatePurchasePersistenceError::NumericOutOfRange("payment_line_no")
                        })?;
                        let account_id = parse_uuid(&payment.account_id, "payment_account_id")?;
                        diesel::insert_into(payments::purchase_payments)
                            .values((
                                payments::household_id.eq(household_id),
                                payments::purchase_id.eq(purchase_id),
                                payments::line_no.eq(line_no),
                                payments::account_id.eq(account_id),
                                payments::amount_yen.eq(payment.amount_yen),
                            ))
                            .execute(&mut *conn)
                            .await?;
                    }
                }

                diesel::update(purchases::purchases.filter(purchases::id.eq(purchase_id)))
                    .set((
                        purchases::occurred_at.eq(occurred_at.unwrap_or(locked.3)),
                        purchases::date_precision
                            .eq(decision.date_precision.as_deref().unwrap_or(&locked.4)),
                        purchases::merchant.eq(decision.merchant.as_deref().unwrap_or(&locked.5)),
                        purchases::payment_account_id.eq(
                            if decision.payment_allocations.is_some() {
                                None
                            } else {
                                locked.7
                            },
                        ),
                        purchases::revision.eq(revision),
                        purchases::field_revisions.eq(changed_fields_json),
                        purchases::updated_at.eq(now),
                    ))
                    .execute(&mut *conn)
                    .await?;

                let event_fields: Vec<Option<String>> =
                    decision.changed_fields.iter().cloned().map(Some).collect();
                diesel::insert_into(events::household_events)
                    .values((
                        events::household_id.eq(household_id),
                        events::event_type.eq("purchase.updated"),
                        events::entity_kind.eq("purchase"),
                        events::entity_id.eq(purchase_id),
                        events::revision.eq(revision),
                        events::changed_fields.eq(event_fields),
                        events::actor_id.eq(actor_id),
                    ))
                    .execute(&mut *conn)
                    .await?;

                Ok(PersistedUpdatePurchase {
                    purchase_id: purchase_id.to_string(),
                    revision: decision.revision,
                })
            },
        )
        .await
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
) -> Result<(), UpdatePurchasePersistenceError> {
    let member = public::household_members::dsl::household_members
        .filter(public::household_members::dsl::household_id.eq(household_id))
        .filter(public::household_members::dsl::member_id.eq(actor_id))
        .filter(public::household_members::dsl::left_at.is_null())
        .select(public::household_members::dsl::member_id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if member {
        Ok(())
    } else {
        Err(UpdatePurchasePersistenceError::MemberNotFound)
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, UpdatePurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| UpdatePurchasePersistenceError::InvalidUuid(field))
}

fn parse_occurred_at(value: &str) -> Result<DateTime<Utc>, UpdatePurchasePersistenceError> {
    DateTime::parse_from_rfc3339(value)
        .map(|value| value.with_timezone(&Utc))
        .map_err(|_| UpdatePurchasePersistenceError::InvalidOccurredAt)
}

fn period_start(value: DateTime<Utc>) -> Result<NaiveDate, UpdatePurchasePersistenceError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(UpdatePurchasePersistenceError::InvalidOccurredAt)?;
    NaiveDate::from_ymd_opt(
        value.with_timezone(&tokyo).year(),
        value.with_timezone(&tokyo).month(),
        1,
    )
    .ok_or(UpdatePurchasePersistenceError::InvalidOccurredAt)
}

fn json_revisions(
    value: serde_json::Value,
) -> Result<BTreeMap<String, u64>, UpdatePurchasePersistenceError> {
    value
        .as_object()
        .ok_or(UpdatePurchasePersistenceError::InvalidDecision(
            UpdatePurchaseError::InvalidRevision,
        ))?
        .iter()
        .map(|(key, value)| {
            let revision =
                value
                    .as_u64()
                    .ok_or(UpdatePurchasePersistenceError::InvalidDecision(
                        UpdatePurchaseError::InvalidRevision,
                    ))?;
            Ok((key.clone(), revision))
        })
        .collect()
}
