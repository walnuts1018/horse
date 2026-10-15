use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_workflows::update_purchase::{
    UpdatePurchaseDecision, UpdatePurchaseDraft, UpdatePurchaseError, decide_update_purchase,
};
use bookkeeping_workflows::{
    MemberFundingBasis, MemberId, MemberWeight, PurchaseStatus, SettlementPolicy,
    decide_post_purchase,
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
    PaymentTotalMismatch,
    InvalidFundingSnapshot,
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
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| UpdatePurchasePersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedUpdatePurchase, UpdatePurchasePersistenceError, _>(
            async move |conn| {
                persist_update_purchase_in_transaction(
                    conn,
                    household_id,
                    actor_id,
                    command,
                    decision,
                    false,
                )
                .await
            },
        )
        .await
}

pub async fn persist_update_purchase_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    command: bookkeeping_workflows::update_purchase::UpdatePurchaseCommand,
    decision: UpdatePurchaseDecision,
    defer_accounting_sync: bool,
) -> Result<PersistedUpdatePurchase, UpdatePurchasePersistenceError> {
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
        revision: u64::try_from(locked.1)
            .map_err(|_| UpdatePurchasePersistenceError::NumericOutOfRange("revision"))?,
        status: locked.2.clone(),
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

    let mut previous_payment_accounts =
        if locked.2 == "POSTED" && decision.payment_allocations.is_some() {
            public::purchase_payments::dsl::purchase_payments
                .filter(public::purchase_payments::dsl::household_id.eq(household_id))
                .filter(public::purchase_payments::dsl::purchase_id.eq(purchase_id))
                .order(public::purchase_payments::dsl::line_no)
                .select((
                    public::purchase_payments::dsl::account_id,
                    public::purchase_payments::dsl::amount_yen,
                ))
                .load::<(Uuid, i64)>(&mut *conn)
                .await?
        } else {
            Vec::new()
        };
    if previous_payment_accounts.is_empty()
        && locked.2 == "POSTED"
        && decision.payment_allocations.is_some()
        && let Some(account_id) = locked.7
    {
        previous_payment_accounts.push((
            account_id,
            purchase_total(&mut *conn, household_id, purchase_id).await?,
        ));
    }
    let previous_payment_snapshots = if decision.payment_allocations.is_some() {
        public::purchase_payment_funding_snapshots::dsl::purchase_payment_funding_snapshots
            .filter(public::purchase_payment_funding_snapshots::dsl::household_id.eq(household_id))
            .filter(public::purchase_payment_funding_snapshots::dsl::purchase_id.eq(purchase_id))
            .select((
                public::purchase_payment_funding_snapshots::dsl::account_id,
                public::purchase_payment_funding_snapshots::dsl::pool_id,
                public::purchase_payment_funding_snapshots::dsl::member_id,
                public::purchase_payment_funding_snapshots::dsl::funding_rule_version,
                public::purchase_payment_funding_snapshots::dsl::funding_weight,
                public::purchase_payment_funding_snapshots::dsl::settlement_policy,
                public::purchase_payment_funding_snapshots::dsl::snapshot_source,
            ))
            .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(&mut *conn)
            .await?
    } else {
        Vec::new()
    };

    if let Some(payment_list) = &decision.payment_allocations {
        if locked.2 == "POSTED" {
            let total = purchase_total(&mut *conn, household_id, purchase_id).await?;
            let paid = payment_list
                .iter()
                .try_fold(0_i64, |total, payment| {
                    total.checked_add(payment.amount_yen)
                })
                .ok_or(UpdatePurchasePersistenceError::PaymentTotalMismatch)?;
            if paid != total {
                return Err(UpdatePurchasePersistenceError::PaymentTotalMismatch);
            }
        }
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
            purchases::date_precision.eq(decision.date_precision.as_deref().unwrap_or(&locked.4)),
            purchases::merchant.eq(decision.merchant.as_deref().unwrap_or(&locked.5)),
            purchases::payment_account_id.eq(if decision.payment_allocations.is_some() {
                None
            } else {
                locked.7
            }),
            purchases::revision.eq(revision),
            purchases::field_revisions.eq(changed_fields_json),
            purchases::updated_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    if locked.2 == "POSTED" && !defer_accounting_sync {
        let accounting_changed = decision.payment_allocations.is_some()
            || occurred_at.is_some_and(|target| target != locked.3);
        if accounting_changed {
            sync_posted_purchase_accounting(
                &mut *conn,
                household_id,
                actor_id,
                purchase_id,
                occurred_at.unwrap_or(locked.3),
                decision.merchant.as_deref().unwrap_or(&locked.5),
                revision,
                &previous_payment_accounts,
                &previous_payment_snapshots,
                decision.payment_allocations.is_some(),
            )
            .await?;
        } else if decision.merchant.is_some() {
            let total = purchase_total(&mut *conn, household_id, purchase_id).await?;
            update_posted_purchase_ledger(
                &mut *conn,
                household_id,
                purchase_id,
                occurred_at.unwrap_or(locked.3),
                decision.merchant.as_deref().unwrap_or(&locked.5),
                total,
            )
            .await?;
        }
    }

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

async fn purchase_total(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
) -> Result<i64, UpdatePurchasePersistenceError> {
    let amounts = public::purchase_items::dsl::purchase_items
        .filter(public::purchase_items::dsl::household_id.eq(household_id))
        .filter(public::purchase_items::dsl::purchase_id.eq(purchase_id))
        .select(public::purchase_items::dsl::amount_yen)
        .load::<i64>(conn)
        .await?;
    amounts
        .into_iter()
        .try_fold(0_i64, |total, amount| total.checked_add(amount))
        .ok_or(UpdatePurchasePersistenceError::PaymentTotalMismatch)
}

async fn sync_posted_purchase_accounting(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    _actor_id: Uuid,
    purchase_id: Uuid,
    occurred_at: DateTime<Utc>,
    merchant: &str,
    revision: i64,
    previous_payment_accounts: &[(Uuid, i64)],
    previous_payment_snapshots: &[(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)],
    payments_changed: bool,
) -> Result<(), UpdatePurchasePersistenceError> {
    let previous_burdens = public::purchase_member_burdens::dsl::purchase_member_burdens
        .filter(public::purchase_member_burdens::dsl::household_id.eq(household_id))
        .filter(public::purchase_member_burdens::dsl::purchase_id.eq(purchase_id))
        .select((
            public::purchase_member_burdens::dsl::item_id,
            public::purchase_member_burdens::dsl::pool_id,
            public::purchase_member_burdens::dsl::member_id,
            public::purchase_member_burdens::dsl::funding_rule_version,
            public::purchase_member_burdens::dsl::funding_weight,
            public::purchase_member_burdens::dsl::settlement_policy,
            public::purchase_member_burdens::dsl::snapshot_source,
        ))
        .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(conn)
        .await?;
    let burden_sources = previous_burdens
        .iter()
        .map(|(item, pool, member, _, _, _, source)| ((*item, *pool, *member), source.clone()))
        .collect::<BTreeMap<_, _>>();
    let mut burden_rows = BTreeMap::<(Uuid, Uuid), Vec<(Uuid, i32, i64, Option<String>)>>::new();
    for (item, pool, member, version, weight, policy, _) in &previous_burdens {
        burden_rows.entry((*item, *pool)).or_default().push((
            *member,
            *version,
            *weight,
            policy.clone(),
        ));
    }
    let burden_bases = burden_rows
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect::<Result<BTreeMap<_, _>, UpdatePurchasePersistenceError>>()?;
    let payment_sources = previous_payment_snapshots
        .iter()
        .map(|(account, pool, member, _, _, _, source)| {
            ((*account, *pool, *member), source.clone())
        })
        .collect::<BTreeMap<_, _>>();
    let mut payment_basis_rows =
        BTreeMap::<(Uuid, Uuid), Vec<(Uuid, i32, i64, Option<String>)>>::new();
    for (account, pool, member, version, weight, policy, _) in previous_payment_snapshots {
        payment_basis_rows
            .entry((*account, *pool))
            .or_default()
            .push((*member, *version, *weight, policy.clone()));
    }
    let payment_bases = payment_basis_rows
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect::<Result<BTreeMap<_, _>, UpdatePurchasePersistenceError>>()?;
    let previous_account_ids = previous_payment_accounts
        .iter()
        .map(|(account, _)| *account)
        .collect::<BTreeSet<_>>();

    let loaded = crate::post_purchase_loader::load_post_purchase_command(
        conn,
        &household_id.to_string(),
        &purchase_id.to_string(),
        u64::try_from(revision)
            .map_err(|_| UpdatePurchasePersistenceError::NumericOutOfRange("revision"))?,
    )
    .await
    .map_err(|error| match error {
        crate::post_purchase_loader::PostPurchaseLoadError::Database(error) => {
            UpdatePurchasePersistenceError::Database(error)
        }
        _ => UpdatePurchasePersistenceError::InvalidDecision(UpdatePurchaseError::Conflict),
    })?;
    let mut command = loaded.command;
    command.draft.status = PurchaseStatus::Draft;
    command.expected_revision = command.draft.revision;
    for allocation in &mut command.allocations {
        let item_id = parse_uuid(allocation.item_id.as_str(), "allocation_item_id")?;
        let pool_id = parse_uuid(allocation.pool_id.as_str(), "allocation_pool_id")?;
        if let Some(basis) = burden_bases.get(&(item_id, pool_id)) {
            allocation.funding = basis.clone();
        }
    }
    for payment in &mut command.payments {
        let account_id = parse_uuid(payment.account_id.as_str(), "payment_account_id")?;
        let pool_id = parse_uuid(payment.funding.pool_id.as_str(), "payment_pool_id")?;
        let basis = payment_bases.get(&(account_id, pool_id)).or_else(|| {
            payment_bases
                .iter()
                .find(|((_, saved_pool), _)| *saved_pool == pool_id)
                .map(|(_, basis)| basis)
        });
        if let Some(basis) = basis {
            payment.funding.rule = basis.clone();
        } else if payments_changed && previous_account_ids.contains(&account_id) {
            return Err(UpdatePurchasePersistenceError::InvalidFundingSnapshot);
        }
    }
    let decision = decide_post_purchase(command).map_err(|_| {
        UpdatePurchasePersistenceError::InvalidDecision(UpdatePurchaseError::Conflict)
    })?;

    let journal_ids = public::journals::dsl::journals
        .filter(public::journals::dsl::household_id.eq(household_id))
        .filter(public::journals::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
        .filter(public::journals::dsl::source_id.eq(purchase_id))
        .select(public::journals::dsl::id)
        .load::<Uuid>(conn)
        .await?;
    if journal_ids.len() != 1 {
        return Err(UpdatePurchasePersistenceError::InvalidDecision(
            UpdatePurchaseError::Conflict,
        ));
    }
    let journal_id = journal_ids[0];
    diesel::update(
        public::journals::dsl::journals.filter(public::journals::dsl::id.eq(journal_id)),
    )
    .set(public::journals::dsl::occurred_at.eq(occurred_at))
    .execute(&mut *conn)
    .await?;
    diesel::delete(
        public::journal_entries::dsl::journal_entries
            .filter(public::journal_entries::dsl::household_id.eq(household_id))
            .filter(public::journal_entries::dsl::journal_id.eq(journal_id)),
    )
    .execute(&mut *conn)
    .await?;
    for line in &decision.journal_lines {
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::household_id.eq(household_id),
                public::journal_entries::journal_id.eq(journal_id),
                public::journal_entries::line_no.eq(i32::try_from(line.line_no).map_err(|_| {
                    UpdatePurchasePersistenceError::NumericOutOfRange("journal_line_no")
                })?),
                public::journal_entries::account_id.eq(line
                    .account_id
                    .as_ref()
                    .map(|id| parse_uuid(id.as_str(), "account_id"))
                    .transpose()?),
                public::journal_entries::pool_id.eq(line
                    .pool_id
                    .as_ref()
                    .map(|id| parse_uuid(id.as_str(), "pool_id"))
                    .transpose()?),
                public::journal_entries::member_id.eq::<Option<Uuid>>(None),
                public::journal_entries::debit_yen.eq(
                    if line.side == bookkeeping_workflows::Side::Debit {
                        line.amount.value()
                    } else {
                        0
                    },
                ),
                public::journal_entries::credit_yen.eq(
                    if line.side == bookkeeping_workflows::Side::Credit {
                        line.amount.value()
                    } else {
                        0
                    },
                ),
                public::journal_entries::memo.eq(line.memo),
            ))
            .execute(&mut *conn)
            .await?;
    }

    diesel::delete(
        public::purchase_member_burdens::dsl::purchase_member_burdens
            .filter(public::purchase_member_burdens::dsl::household_id.eq(household_id))
            .filter(public::purchase_member_burdens::dsl::purchase_id.eq(purchase_id)),
    )
    .execute(&mut *conn)
    .await?;
    for snapshot in &decision.member_burden_snapshots {
        let item_id = parse_uuid(snapshot.item_id.as_str(), "item_id")?;
        let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        let source = burden_sources
            .get(&(item_id, pool_id, member_id))
            .map(String::as_str)
            .unwrap_or("POSTED");
        diesel::insert_into(public::purchase_member_burdens::table)
            .values((
                public::purchase_member_burdens::household_id.eq(household_id),
                public::purchase_member_burdens::purchase_id.eq(purchase_id),
                public::purchase_member_burdens::item_id.eq(item_id),
                public::purchase_member_burdens::pool_id.eq(pool_id),
                public::purchase_member_burdens::member_id.eq(member_id),
                public::purchase_member_burdens::amount_yen.eq(snapshot.amount.value()),
                public::purchase_member_burdens::funding_rule_version.eq(i32::try_from(
                    snapshot.funding_rule_version,
                )
                .map_err(|_| {
                    UpdatePurchasePersistenceError::NumericOutOfRange("funding_rule_version")
                })?),
                public::purchase_member_burdens::funding_weight.eq(i64::try_from(
                    snapshot.funding_weight,
                )
                .map_err(|_| {
                    UpdatePurchasePersistenceError::NumericOutOfRange("funding_weight")
                })?),
                public::purchase_member_burdens::settlement_policy
                    .eq(snapshot.settlement_policy.as_deref()),
                public::purchase_member_burdens::snapshot_source.eq(source),
            ))
            .execute(&mut *conn)
            .await?;
    }
    if payments_changed {
        diesel::delete(
            public::purchase_payment_funding_snapshots::dsl::purchase_payment_funding_snapshots
                .filter(
                    public::purchase_payment_funding_snapshots::dsl::household_id.eq(household_id),
                )
                .filter(
                    public::purchase_payment_funding_snapshots::dsl::purchase_id.eq(purchase_id),
                ),
        )
        .execute(&mut *conn)
        .await?;
        for snapshot in &decision.payment_funding_snapshots {
            let account_id = parse_uuid(snapshot.account_id.as_str(), "account_id")?;
            let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
            let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
            let source = payment_sources
                .get(&(account_id, pool_id, member_id))
                .or_else(|| {
                    payment_sources
                        .iter()
                        .find(|((_, saved_pool, saved_member), _)| {
                            *saved_pool == pool_id && *saved_member == member_id
                        })
                        .map(|(_, source)| source)
                })
                .map(String::as_str)
                .unwrap_or("POSTED");
            diesel::insert_into(public::purchase_payment_funding_snapshots::table)
                .values((
                    public::purchase_payment_funding_snapshots::household_id.eq(household_id),
                    public::purchase_payment_funding_snapshots::purchase_id.eq(purchase_id),
                    public::purchase_payment_funding_snapshots::payment_line_no.eq(i32::try_from(
                        snapshot.line_no,
                    )
                    .map_err(|_| {
                        UpdatePurchasePersistenceError::NumericOutOfRange("payment_line_no")
                    })?),
                    public::purchase_payment_funding_snapshots::account_id.eq(account_id),
                    public::purchase_payment_funding_snapshots::pool_id.eq(pool_id),
                    public::purchase_payment_funding_snapshots::member_id.eq(member_id),
                    public::purchase_payment_funding_snapshots::funding_rule_version.eq(
                        i32::try_from(snapshot.funding_rule_version).map_err(|_| {
                            UpdatePurchasePersistenceError::NumericOutOfRange(
                                "funding_rule_version",
                            )
                        })?,
                    ),
                    public::purchase_payment_funding_snapshots::funding_weight.eq(i64::try_from(
                        snapshot.funding_weight,
                    )
                    .map_err(|_| {
                        UpdatePurchasePersistenceError::NumericOutOfRange("funding_weight")
                    })?),
                    public::purchase_payment_funding_snapshots::settlement_policy
                        .eq(snapshot.settlement_policy.as_deref()),
                    public::purchase_payment_funding_snapshots::snapshot_source.eq(source),
                ))
                .execute(&mut *conn)
                .await?;
        }
    }
    let total = decision
        .journal_lines
        .iter()
        .filter(|line| line.side == bookkeeping_workflows::Side::Debit)
        .try_fold(0_i64, |total, line| total.checked_add(line.amount.value()))
        .ok_or(UpdatePurchasePersistenceError::NumericOutOfRange(
            "purchase_total",
        ))?;
    update_posted_purchase_ledger(
        conn,
        household_id,
        purchase_id,
        occurred_at,
        merchant,
        total,
    )
    .await?;
    Ok(())
}

async fn update_posted_purchase_ledger(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    occurred_at: DateTime<Utc>,
    merchant: &str,
    amount_yen: i64,
) -> Result<(), UpdatePurchasePersistenceError> {
    let updated = diesel::update(
        public::ledger_entries::dsl::ledger_entries
            .filter(public::ledger_entries::dsl::household_id.eq(household_id))
            .filter(public::ledger_entries::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
            .filter(public::ledger_entries::dsl::source_id.eq(purchase_id)),
    )
    .set((
        public::ledger_entries::dsl::occurred_at.eq(occurred_at),
        public::ledger_entries::dsl::description.eq(merchant),
        public::ledger_entries::dsl::amount_yen.eq(amount_yen),
    ))
    .execute(conn)
    .await?;
    if updated == 0 {
        return Err(UpdatePurchasePersistenceError::InvalidDecision(
            UpdatePurchaseError::Conflict,
        ));
    }
    Ok(())
}

fn funding_basis(
    rows: Vec<(Uuid, i32, i64, Option<String>)>,
) -> Result<MemberFundingBasis, UpdatePurchasePersistenceError> {
    let Some((_, version, _, policy)) = rows.as_slice().first() else {
        return Err(UpdatePurchasePersistenceError::InvalidFundingSnapshot);
    };
    if rows
        .iter()
        .any(|(_, row_version, _, row_policy)| row_version != version || row_policy != policy)
    {
        return Err(UpdatePurchasePersistenceError::InvalidFundingSnapshot);
    }
    let version = u32::try_from(*version)
        .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)?;
    if let Some(policy) = policy {
        let policy = match policy.as_str() {
            "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
            "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
            _ => return Err(UpdatePurchasePersistenceError::InvalidFundingSnapshot),
        };
        let weights = rows
            .into_iter()
            .map(|(member_id, _, weight, _)| {
                Ok(MemberWeight::new(
                    MemberId::new(member_id.to_string())
                        .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)?,
                    u64::try_from(weight)
                        .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)?,
                )
                .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)?)
            })
            .collect::<Result<Vec<_>, UpdatePurchasePersistenceError>>()?;
        MemberFundingBasis::shared(version, weights, policy)
            .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)
    } else {
        if rows.len() != 1 || rows[0].2 != 1 {
            return Err(UpdatePurchasePersistenceError::InvalidFundingSnapshot);
        }
        MemberFundingBasis::personal(
            version,
            MemberId::new(rows[0].0.to_string())
                .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)?,
        )
        .map_err(|_| UpdatePurchasePersistenceError::InvalidFundingSnapshot)
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, UpdatePurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| UpdatePurchasePersistenceError::InvalidUuid(field))
}

pub(crate) fn parse_occurred_at(
    value: &str,
) -> Result<DateTime<Utc>, UpdatePurchasePersistenceError> {
    DateTime::parse_from_rfc3339(value)
        .map(|value| value.with_timezone(&Utc))
        .map_err(|_| UpdatePurchasePersistenceError::InvalidOccurredAt)
}

pub(crate) fn period_start(
    value: DateTime<Utc>,
) -> Result<NaiveDate, UpdatePurchasePersistenceError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(UpdatePurchasePersistenceError::InvalidOccurredAt)?;
    NaiveDate::from_ymd_opt(
        value.with_timezone(&tokyo).year(),
        value.with_timezone(&tokyo).month(),
        1,
    )
    .ok_or(UpdatePurchasePersistenceError::InvalidOccurredAt)
}

pub(crate) fn json_revisions(
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
