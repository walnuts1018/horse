use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_workflows::{PostPurchaseDecision, PostingTarget, Side};
use chrono::{DateTime, Datelike, FixedOffset, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, post_purchase_loader::ResolvedPostPurchasePayment, schema::public};

const OPERATION: &str = "post_purchase";

#[derive(Debug, Clone)]
pub struct PersistPostPurchase {
    pub household_id: String,
    pub actor_id: String,
    pub expected_revision: u64,
    pub ledger_description: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
    pub resolved_payments: Vec<ResolvedPostPurchasePayment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedPostPurchase {
    pub purchase_id: String,
    pub journal_id: String,
    pub ledger_entry_id: String,
}

#[derive(Debug)]
pub enum PostPurchasePersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    InvalidIdempotencyKey,
    IdempotencyConflict,
    AccountingPeriodClosed,
    StalePurchase,
    HouseholdMismatch,
    InvalidDecision,
}

impl From<DieselError> for PostPurchasePersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn persist_post_purchase(
    pool: &PgPool,
    input: PersistPostPurchase,
    decision: PostPurchaseDecision,
) -> Result<PersistedPostPurchase, PostPurchasePersistenceError> {
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PostPurchasePersistenceError::Pool(error.to_string()))?;
    (&mut *pooled)
        .transaction::<PersistedPostPurchase, PostPurchasePersistenceError, _>(async |conn| {
            persist_post_purchase_in_transaction(conn, input, decision).await
        })
        .await
}

pub async fn find_idempotent_post_purchase(
    pool: &PgPool,
    household_id: Uuid,
    idempotency_key: &str,
    request_fingerprint: [u8; 32],
) -> Result<Option<PersistedPostPurchase>, PostPurchasePersistenceError> {
    use public::idempotency_records::dsl as idem;
    use public::journals::dsl as journals;
    use public::ledger_entries::dsl as ledger;

    let mut conn = pool
        .get()
        .await
        .map_err(|error| PostPurchasePersistenceError::Pool(error.to_string()))?;
    let existing = idem::idempotency_records
        .filter(idem::household_id.eq(household_id))
        .filter(idem::operation.eq(OPERATION))
        .filter(idem::key.eq(idempotency_key))
        .select((
            idem::request_fingerprint,
            idem::resource_kind,
            idem::resource_id,
        ))
        .first::<(Vec<u8>, String, Uuid)>(&mut *conn)
        .await
        .optional()?;
    let Some((fingerprint, resource_kind, purchase_id)) = existing else {
        return Ok(None);
    };
    if fingerprint.as_slice() != request_fingerprint || resource_kind != "purchase" {
        return Err(PostPurchasePersistenceError::IdempotencyConflict);
    }
    let journal_id = journals::journals
        .filter(journals::household_id.eq(household_id))
        .filter(journals::source_kind.eq("purchase"))
        .filter(journals::source_id.eq(purchase_id))
        .select(journals::id)
        .first::<Uuid>(&mut *conn)
        .await?;
    let ledger_entry_id = ledger::ledger_entries
        .filter(ledger::household_id.eq(household_id))
        .filter(ledger::journal_id.eq(journal_id))
        .select(ledger::id)
        .first::<Uuid>(&mut *conn)
        .await?;
    Ok(Some(PersistedPostPurchase {
        purchase_id: purchase_id.to_string(),
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    }))
}

pub async fn persist_post_purchase_in_transaction(
    conn: &mut diesel_async::AsyncPgConnection,
    input: PersistPostPurchase,
    decision: PostPurchaseDecision,
) -> Result<PersistedPostPurchase, PostPurchasePersistenceError> {
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(PostPurchasePersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let purchase_id = parse_uuid(decision.purchase_id().as_str(), "purchase_id")?;
    let expected_revision = i64::try_from(input.expected_revision)
        .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("expected_revision"))?;
    let new_revision = i64::try_from(decision.new_revision())
        .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("new_revision"))?;
    let journal_id = Uuid::new_v4();
    let ledger_entry_id = Uuid::new_v4();
    validate_decision(&decision, expected_revision)?;

    use public::household_members::dsl as members;
    use public::idempotency_records::dsl as idem;
    use public::journals::dsl as journals;
    use public::ledger_entries::dsl as ledger;
    use public::purchases::dsl as purchases;

    let actor_is_member = members::household_members
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !actor_is_member {
        return Err(PostPurchasePersistenceError::HouseholdMismatch);
    }

    let inserted = diesel::insert_into(idem::idempotency_records)
        .values((
            idem::household_id.eq(household_id),
            idem::operation.eq(OPERATION),
            idem::key.eq(&input.idempotency_key),
            idem::request_fingerprint.eq(input.request_fingerprint.to_vec()),
            idem::resource_kind.eq("purchase"),
            idem::resource_id.eq(purchase_id),
        ))
        .on_conflict((idem::household_id, idem::operation, idem::key))
        .do_nothing()
        .returning(idem::resource_id)
        .get_result::<Uuid>(&mut *conn)
        .await
        .optional()?;

    if inserted.is_none() {
        let (fingerprint, resource_kind, resource_id) = idem::idempotency_records
            .filter(idem::household_id.eq(household_id))
            .filter(idem::operation.eq(OPERATION))
            .filter(idem::key.eq(&input.idempotency_key))
            .select((
                idem::request_fingerprint,
                idem::resource_kind,
                idem::resource_id,
            ))
            .first::<(Vec<u8>, String, Uuid)>(&mut *conn)
            .await?;
        if fingerprint.as_slice() != input.request_fingerprint
            || resource_kind != "purchase"
            || resource_id != purchase_id
        {
            return Err(PostPurchasePersistenceError::IdempotencyConflict);
        }
        let saved_journal_id = journals::journals
            .filter(journals::household_id.eq(household_id))
            .filter(journals::source_kind.eq("purchase"))
            .filter(journals::source_id.eq(resource_id))
            .select(journals::id)
            .first::<Uuid>(&mut *conn)
            .await?;
        let saved_ledger_entry_id = ledger::ledger_entries
            .filter(ledger::household_id.eq(household_id))
            .filter(ledger::journal_id.eq(saved_journal_id))
            .select(ledger::id)
            .first::<Uuid>(&mut *conn)
            .await?;
        return Ok(PersistedPostPurchase {
            purchase_id: resource_id.to_string(),
            journal_id: saved_journal_id.to_string(),
            ledger_entry_id: saved_ledger_entry_id.to_string(),
        });
    }

    validate_household_references(&mut *conn, household_id, actor_id, purchase_id, &decision)
        .await?;
    materialize_fallback_payments(
        &mut *conn,
        household_id,
        purchase_id,
        &input.resolved_payments,
    )
    .await?;
    validate_payment_rows(
        &mut *conn,
        household_id,
        purchase_id,
        &input.resolved_payments,
        &decision,
    )
    .await?;
    validate_member_burden_snapshots(&mut *conn, household_id, purchase_id, &decision).await?;
    let occurred_at = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq(purchase_id))
        .select(purchases::occurred_at)
        .first::<DateTime<Utc>>(&mut *conn)
        .await?;
    let tokyo =
        FixedOffset::east_opt(9 * 60 * 60).ok_or(PostPurchasePersistenceError::InvalidDecision)?;
    let period_start = occurred_at
        .with_timezone(&tokyo)
        .date_naive()
        .with_day(1)
        .ok_or(PostPurchasePersistenceError::InvalidDecision)?;
    ensure_period_open(&mut *conn, household_id, period_start).await?;
    update_purchase(
        &mut *conn,
        household_id,
        purchase_id,
        expected_revision,
        new_revision,
        occurred_at,
    )
    .await?;

    diesel::insert_into(journals::journals)
        .values((
            journals::id.eq(journal_id),
            journals::household_id.eq(household_id),
            journals::source_kind.eq("purchase"),
            journals::source_id.eq(purchase_id),
            journals::occurred_at.eq(occurred_at),
            journals::created_by.eq(actor_id),
            journals::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    for line in &decision.journal_lines() {
        let line_no = i32::try_from(line.line_no())
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal line_no"))?;
        let amount = i64::try_from(line.amount().value())
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal amount"))?;
        let (account_id, pool_id) = match line.target() {
            PostingTarget::Account(id) => (Some(parse_uuid(id.as_str(), "account_id")?), None),
            PostingTarget::Pool(id) => (None, Some(parse_uuid(id.as_str(), "pool_id")?)),
        };
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::household_id.eq(household_id),
                public::journal_entries::journal_id.eq(journal_id),
                public::journal_entries::line_no.eq(line_no),
                public::journal_entries::account_id.eq(account_id),
                public::journal_entries::pool_id.eq(pool_id),
                public::journal_entries::member_id.eq::<Option<Uuid>>(None),
                public::journal_entries::debit_yen.eq(if line.side() == Side::Debit {
                    amount
                } else {
                    0
                }),
                public::journal_entries::credit_yen.eq(if line.side() == Side::Credit {
                    amount
                } else {
                    0
                }),
                public::journal_entries::memo.eq(line.memo()),
            ))
            .execute(&mut *conn)
            .await?;
    }

    for snapshot in &decision.member_burden_snapshots() {
        let amount = snapshot.amount.value();
        let rule_version = i32::try_from(snapshot.funding_rule_version)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_rule_version"))?;
        let weight = i64::try_from(snapshot.funding_weight)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_weight"))?;
        diesel::insert_into(public::purchase_member_burdens::table)
            .values((
                public::purchase_member_burdens::household_id.eq(household_id),
                public::purchase_member_burdens::purchase_id.eq(purchase_id),
                public::purchase_member_burdens::item_id
                    .eq(parse_uuid(snapshot.item_id.as_str(), "item_id")?),
                public::purchase_member_burdens::pool_id
                    .eq(parse_uuid(snapshot.pool_id.as_str(), "pool_id")?),
                public::purchase_member_burdens::member_id
                    .eq(parse_uuid(snapshot.member_id.as_str(), "member_id")?),
                public::purchase_member_burdens::amount_yen.eq(amount),
                public::purchase_member_burdens::funding_rule_version.eq(rule_version),
                public::purchase_member_burdens::funding_weight.eq(weight),
                public::purchase_member_burdens::settlement_policy
                    .eq(snapshot.settlement_policy.as_deref()),
                public::purchase_member_burdens::snapshot_source.eq("POSTED"),
            ))
            .execute(&mut *conn)
            .await?;
    }

    for snapshot in &decision.payment_funding_snapshots() {
        let line_no = i32::try_from(snapshot.line_no)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("payment line_no"))?;
        let rule_version = i32::try_from(snapshot.funding_rule_version)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_rule_version"))?;
        let weight = i64::try_from(snapshot.funding_weight)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_weight"))?;
        diesel::insert_into(public::purchase_payment_funding_snapshots::table)
            .values((
                public::purchase_payment_funding_snapshots::household_id.eq(household_id),
                public::purchase_payment_funding_snapshots::purchase_id.eq(purchase_id),
                public::purchase_payment_funding_snapshots::payment_line_no.eq(line_no),
                public::purchase_payment_funding_snapshots::account_id
                    .eq(parse_uuid(snapshot.account_id.as_str(), "account_id")?),
                public::purchase_payment_funding_snapshots::pool_id
                    .eq(parse_uuid(snapshot.pool_id.as_str(), "pool_id")?),
                public::purchase_payment_funding_snapshots::member_id
                    .eq(parse_uuid(snapshot.member_id.as_str(), "member_id")?),
                public::purchase_payment_funding_snapshots::funding_rule_version.eq(rule_version),
                public::purchase_payment_funding_snapshots::funding_weight.eq(weight),
                public::purchase_payment_funding_snapshots::settlement_policy
                    .eq(snapshot.settlement_policy.as_deref()),
                public::purchase_payment_funding_snapshots::snapshot_source.eq("POSTED"),
            ))
            .execute(&mut *conn)
            .await?;
    }

    let amount_yen = ledger_amount(&decision)?;
    diesel::insert_into(ledger::ledger_entries)
        .values((
            ledger::id.eq(ledger_entry_id),
            ledger::household_id.eq(household_id),
            ledger::journal_id.eq(journal_id),
            ledger::source_kind.eq("purchase"),
            ledger::source_id.eq(purchase_id),
            ledger::occurred_at.eq(occurred_at),
            ledger::description.eq(&input.ledger_description),
            ledger::amount_yen.eq(amount_yen),
            ledger::created_at.eq(now),
        ))
        .execute(&mut *conn)
        .await?;

    for event in &decision.events() {
        let event_revision = i64::try_from(event.revision)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("event revision"))?;
        let changed_fields = event
            .changed_fields
            .iter()
            .map(|field| Some((*field).to_owned()))
            .collect::<Vec<_>>();
        diesel::insert_into(public::household_events::table)
            .values((
                public::household_events::household_id.eq(household_id),
                public::household_events::event_type.eq(event.event_type),
                public::household_events::entity_kind.eq("purchase"),
                public::household_events::entity_id
                    .eq(parse_uuid(event.entity_id.as_str(), "event entity_id")?),
                public::household_events::revision.eq(event_revision),
                public::household_events::changed_fields.eq(changed_fields),
                public::household_events::actor_id.eq(actor_id),
                public::household_events::created_at.eq(now),
            ))
            .execute(&mut *conn)
            .await?;
    }

    Ok(PersistedPostPurchase {
        purchase_id: purchase_id.to_string(),
        journal_id: journal_id.to_string(),
        ledger_entry_id: ledger_entry_id.to_string(),
    })
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, PostPurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| PostPurchasePersistenceError::InvalidUuid(field))
}

fn validate_decision(
    decision: &PostPurchaseDecision,
    expected_revision: i64,
) -> Result<(), PostPurchasePersistenceError> {
    if decision.new_status() != bookkeeping_workflows::PurchaseStatus::Posted
        || decision.events().is_empty()
        || expected_revision.checked_add(1) != i64::try_from(decision.new_revision()).ok()
    {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    for event in &decision.events() {
        if event.entity_id != decision.purchase_id() || event.revision != decision.new_revision() {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
    }
    let mut lines = BTreeSet::new();
    let mut debit = 0_i64;
    let mut credit = 0_i64;
    for line in &decision.journal_lines() {
        let number = i32::try_from(line.line_no())
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal line_no"))?;
        if number <= 0 || !lines.insert(number) {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        let amount = i64::try_from(line.amount().value())
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal amount"))?;
        match line.side() {
            Side::Debit => {
                debit = debit.checked_add(amount).ok_or(
                    PostPurchasePersistenceError::NumericOutOfRange("debit total"),
                )?
            }
            Side::Credit => {
                credit = credit.checked_add(amount).ok_or(
                    PostPurchasePersistenceError::NumericOutOfRange("credit total"),
                )?
            }
        }
    }
    if debit == 0 || debit != credit || debit > 1_000_000_000_000 {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    // 明細の相手先の組み合わせ(プールか口座のどちらか一つ)はPostingTarget型で保証されるため、ここでは再検査しない。
    Ok(())
}

fn ledger_amount(decision: &PostPurchaseDecision) -> Result<i64, PostPurchasePersistenceError> {
    let total = decision
        .journal_lines
        .iter()
        .filter(|line| line.side() == Side::Debit)
        .try_fold(0_i64, |sum, line| {
            sum.checked_add(line.amount().value()).ok_or(
                PostPurchasePersistenceError::NumericOutOfRange("ledger amount"),
            )
        })?;
    if total <= 0 || total > 1_000_000_000_000 {
        return Err(PostPurchasePersistenceError::NumericOutOfRange(
            "ledger amount",
        ));
    }
    Ok(total)
}

async fn ensure_period_open(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    period_start: chrono::NaiveDate,
) -> Result<(), PostPurchasePersistenceError> {
    use public::accounting_periods::dsl as periods;
    diesel::insert_into(periods::accounting_periods)
        .values((
            periods::household_id.eq(household_id),
            periods::period_start.eq(period_start),
            periods::status.eq("OPEN"),
            periods::revision.eq(0_i64),
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
        .first::<String>(conn)
        .await
        .optional()?;
    if status.as_deref() != Some("OPEN") {
        return Err(PostPurchasePersistenceError::AccountingPeriodClosed);
    }
    Ok(())
}

async fn update_purchase(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    expected_revision: i64,
    new_revision: i64,
    occurred_at: DateTime<Utc>,
) -> Result<(), PostPurchasePersistenceError> {
    use public::purchases::dsl as purchases;
    let changed = diesel::update(
        purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::id.eq(purchase_id))
            .filter(purchases::revision.eq(expected_revision))
            .filter(purchases::status.eq("DRAFT"))
            .filter(purchases::occurred_at.eq(occurred_at)),
    )
    .set((
        purchases::status.eq("POSTED"),
        purchases::revision.eq(new_revision),
        purchases::updated_at.eq(now),
    ))
    .execute(conn)
    .await?;
    if changed != 1 {
        return Err(PostPurchasePersistenceError::StalePurchase);
    }
    Ok(())
}

async fn validate_household_references(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    actor_id: Uuid,
    purchase_id: Uuid,
    decision: &PostPurchaseDecision,
) -> Result<(), PostPurchasePersistenceError> {
    use public::{accounts, household_members, pools, purchase_items};

    let mut pool_ids = BTreeSet::new();
    let mut account_ids = BTreeSet::new();
    let mut member_ids = BTreeSet::from([actor_id]);
    let mut item_ids = BTreeSet::new();
    for line in &decision.journal_lines() {
        match line.target() {
            PostingTarget::Pool(id) => {
                pool_ids.insert(parse_uuid(id.as_str(), "pool_id")?);
            }
            PostingTarget::Account(id) => {
                account_ids.insert(parse_uuid(id.as_str(), "account_id")?);
            }
        }
    }
    for snapshot in &decision.member_burden_snapshots() {
        item_ids.insert(parse_uuid(snapshot.item_id.as_str(), "item_id")?);
        pool_ids.insert(parse_uuid(snapshot.pool_id.as_str(), "pool_id")?);
        member_ids.insert(parse_uuid(snapshot.member_id.as_str(), "member_id")?);
    }
    let mut account_pool_pairs = BTreeSet::new();
    for snapshot in &decision.payment_funding_snapshots() {
        let account_id = parse_uuid(snapshot.account_id.as_str(), "account_id")?;
        let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        account_ids.insert(account_id);
        pool_ids.insert(pool_id);
        member_ids.insert(parse_uuid(snapshot.member_id.as_str(), "member_id")?);
        account_pool_pairs.insert((account_id, pool_id));
    }

    let found_pools = pools::table
        .filter(pools::household_id.eq(household_id))
        .filter(pools::id.eq_any(pool_ids.iter().copied()))
        .select(pools::id)
        .load::<Uuid>(conn)
        .await?;
    if found_pools.len() != pool_ids.len() {
        return Err(PostPurchasePersistenceError::HouseholdMismatch);
    }
    let found_accounts = accounts::table
        .filter(accounts::household_id.eq(household_id))
        .filter(accounts::id.eq_any(account_ids.iter().copied()))
        .select((accounts::id, accounts::pool_id))
        .load::<(Uuid, Option<Uuid>)>(conn)
        .await?;
    if found_accounts.len() != account_ids.len()
        || account_pool_pairs
            .iter()
            .any(|pair| !found_accounts.contains(&(pair.0, Some(pair.1))))
    {
        return Err(PostPurchasePersistenceError::HouseholdMismatch);
    }
    let found_members = household_members::table
        .filter(household_members::household_id.eq(household_id))
        .filter(household_members::member_id.eq_any(member_ids.iter().copied()))
        .select(household_members::member_id)
        .load::<Uuid>(conn)
        .await?;
    if found_members.len() != member_ids.len() {
        return Err(PostPurchasePersistenceError::HouseholdMismatch);
    }
    let found_items = purchase_items::table
        .filter(purchase_items::household_id.eq(household_id))
        .filter(purchase_items::purchase_id.eq(purchase_id))
        .filter(purchase_items::id.eq_any(item_ids.iter().copied()))
        .select(purchase_items::id)
        .load::<Uuid>(conn)
        .await?;
    if found_items.len() != item_ids.len() {
        return Err(PostPurchasePersistenceError::HouseholdMismatch);
    }
    Ok(())
}

async fn materialize_fallback_payments(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    payments: &[ResolvedPostPurchasePayment],
) -> Result<(), PostPurchasePersistenceError> {
    use public::purchase_payments::dsl as payments_table;

    let fallback_payments = payments
        .iter()
        .filter(|payment| payment.is_fallback)
        .collect::<Vec<_>>();
    if fallback_payments.is_empty() {
        return Ok(());
    }
    let existing = payments_table::purchase_payments
        .filter(payments_table::household_id.eq(household_id))
        .filter(payments_table::purchase_id.eq(purchase_id))
        .select(payments_table::line_no)
        .load::<i32>(conn)
        .await?;
    if !existing.is_empty() {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    let mut line_numbers = BTreeSet::new();
    for payment in fallback_payments {
        let line_no = i32::try_from(payment.line_no)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("payment line_no"))?;
        let account_id = parse_uuid(payment.account_id.as_str(), "payment account_id")?;
        if line_no <= 0 || payment.amount.value() <= 0 || !line_numbers.insert(line_no) {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        diesel::insert_into(payments_table::purchase_payments)
            .values((
                payments_table::household_id.eq(household_id),
                payments_table::purchase_id.eq(purchase_id),
                payments_table::line_no.eq(line_no),
                payments_table::account_id.eq(account_id),
                payments_table::amount_yen.eq(payment.amount.value()),
            ))
            .execute(conn)
            .await?;
    }
    Ok(())
}

async fn validate_payment_rows(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    resolved_payments: &[ResolvedPostPurchasePayment],
    decision: &PostPurchaseDecision,
) -> Result<(), PostPurchasePersistenceError> {
    use public::purchase_payments::dsl as payments;

    let persisted = payments::purchase_payments
        .filter(payments::household_id.eq(household_id))
        .filter(payments::purchase_id.eq(purchase_id))
        .select((
            payments::line_no,
            payments::account_id,
            payments::amount_yen,
        ))
        .load::<(i32, Uuid, i64)>(conn)
        .await?
        .into_iter()
        .collect::<BTreeSet<_>>();
    let expected = resolved_payments
        .iter()
        .map(|payment| {
            let line_no = i32::try_from(payment.line_no)
                .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("payment line_no"))?;
            Ok((
                line_no,
                parse_uuid(payment.account_id.as_str(), "payment account_id")?,
                payment.amount.value(),
            ))
        })
        .collect::<Result<BTreeSet<_>, PostPurchasePersistenceError>>()?;
    let expected_identity = expected
        .iter()
        .map(|(line_no, account_id, _)| (*line_no, *account_id))
        .collect::<BTreeSet<_>>();
    let decision_identity = decision
        .payment_funding_snapshots
        .iter()
        .map(|snapshot| {
            Ok((
                i32::try_from(snapshot.line_no).map_err(|_| {
                    PostPurchasePersistenceError::NumericOutOfRange("payment line_no")
                })?,
                parse_uuid(snapshot.account_id.as_str(), "payment account_id")?,
            ))
        })
        .collect::<Result<BTreeSet<_>, PostPurchasePersistenceError>>()?;
    if persisted != expected
        || expected.len() != resolved_payments.len()
        || expected_identity.len() != expected.len()
        || expected_identity != decision_identity
    {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }

    let mut snapshots_by_payment = BTreeMap::<(i32, Uuid), (BTreeSet<Uuid>, i64)>::new();
    for snapshot in &decision.payment_funding_snapshots() {
        let line_no = i32::try_from(snapshot.line_no)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("payment line_no"))?;
        let account_id = parse_uuid(snapshot.account_id.as_str(), "account_id")?;
        let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        let rule_version = i32::try_from(snapshot.funding_rule_version)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_rule_version"))?;
        let weight = i64::try_from(snapshot.funding_weight)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_weight"))?;
        if line_no <= 0 || rule_version <= 0 || weight <= 0 {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        let (members, total_weight) = snapshots_by_payment
            .entry((line_no, account_id))
            .or_insert_with(|| (BTreeSet::new(), 0));
        if !members.insert(member_id) {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        *total_weight = total_weight.checked_add(weight).ok_or(
            PostPurchasePersistenceError::NumericOutOfRange("payment funding weight"),
        )?;
    }
    if snapshots_by_payment.len() != expected_identity.len()
        || snapshots_by_payment
            .keys()
            .copied()
            .collect::<BTreeSet<_>>()
            != expected_identity
    {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }

    validate_journal_rows(&persisted, decision)?;
    Ok(())
}

async fn validate_member_burden_snapshots(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    decision: &PostPurchaseDecision,
) -> Result<(), PostPurchasePersistenceError> {
    use public::purchase_allocations::dsl as allocations;
    use public::purchase_items::dsl as items;

    let persisted = allocations::purchase_allocations
        .filter(allocations::household_id.eq(household_id))
        .filter(allocations::purchase_id.eq(purchase_id))
        .select((
            allocations::item_id,
            allocations::pool_id,
            allocations::amount_yen,
        ))
        .load::<(Uuid, Uuid, i64)>(conn)
        .await?
        .into_iter()
        .map(|(item_id, pool_id, amount)| ((item_id, pool_id), amount))
        .collect::<BTreeMap<_, _>>();
    if persisted.is_empty() {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }

    let item_amounts = items::purchase_items
        .filter(items::household_id.eq(household_id))
        .filter(items::purchase_id.eq(purchase_id))
        .select((items::id, items::amount_yen))
        .load::<(Uuid, i64)>(conn)
        .await?
        .into_iter()
        .collect::<BTreeMap<_, _>>();
    let mut allocated_by_item = BTreeMap::<Uuid, i64>::new();
    for ((item_id, _), amount) in &persisted {
        if *amount < 0 || !item_amounts.contains_key(item_id) {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        let total = allocated_by_item.entry(*item_id).or_insert(0);
        *total =
            total
                .checked_add(*amount)
                .ok_or(PostPurchasePersistenceError::NumericOutOfRange(
                    "item allocation total",
                ))?;
    }
    if allocated_by_item != item_amounts {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }

    let mut snapshots = BTreeMap::<(Uuid, Uuid), i64>::new();
    let mut snapshot_members = BTreeSet::new();
    for snapshot in &decision.member_burden_snapshots() {
        let item_id = parse_uuid(snapshot.item_id.as_str(), "item_id")?;
        let pool_id = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        let member_id = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        let rule_version = i32::try_from(snapshot.funding_rule_version)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_rule_version"))?;
        let weight = i64::try_from(snapshot.funding_weight)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("funding_weight"))?;
        if snapshot.amount.value() < 0 || rule_version <= 0 || weight <= 0 {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        if !snapshot_members.insert((item_id, pool_id, member_id)) {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        let total = snapshots.entry((item_id, pool_id)).or_insert(0);
        *total = total.checked_add(snapshot.amount.value()).ok_or(
            PostPurchasePersistenceError::NumericOutOfRange("member burden snapshot total"),
        )?;
    }
    if snapshots != persisted {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    validate_journal_debits(&persisted, decision)?;
    Ok(())
}

fn validate_journal_rows(
    persisted_payments: &BTreeSet<(i32, Uuid, i64)>,
    decision: &PostPurchaseDecision,
) -> Result<(), PostPurchasePersistenceError> {
    let mut expected_credits = BTreeMap::<Uuid, i64>::new();
    for (_, account_id, amount) in persisted_payments {
        let total = expected_credits.entry(*account_id).or_insert(0);
        *total =
            total
                .checked_add(*amount)
                .ok_or(PostPurchasePersistenceError::NumericOutOfRange(
                    "payment total",
                ))?;
    }
    let mut actual_credits = BTreeMap::<Uuid, i64>::new();
    for line in &decision.journal_lines() {
        let amount = i64::try_from(line.amount().value())
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal amount"))?;
        let id = match line.side() {
            Side::Debit => continue,
            Side::Credit => match line.target() {
                PostingTarget::Account(id) => parse_uuid(id.as_str(), "account_id")?,
                PostingTarget::Pool(_) => {
                    return Err(PostPurchasePersistenceError::InvalidDecision);
                }
            },
        };
        let total = actual_credits.entry(id).or_insert(0);
        *total =
            total
                .checked_add(amount)
                .ok_or(PostPurchasePersistenceError::NumericOutOfRange(
                    "journal line total",
                ))?;
    }

    if actual_credits != expected_credits {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    Ok(())
}

fn validate_journal_debits(
    persisted_allocations: &BTreeMap<(Uuid, Uuid), i64>,
    decision: &PostPurchaseDecision,
) -> Result<(), PostPurchasePersistenceError> {
    let mut expected = BTreeMap::<Uuid, i64>::new();
    for ((_, pool_id), amount) in persisted_allocations {
        if *amount == 0 {
            continue;
        }
        let total = expected.entry(*pool_id).or_insert(0);
        *total =
            total
                .checked_add(*amount)
                .ok_or(PostPurchasePersistenceError::NumericOutOfRange(
                    "allocation total",
                ))?;
    }
    let mut actual = BTreeMap::<Uuid, i64>::new();
    for line in decision
        .journal_lines
        .iter()
        .filter(|line| line.side() == Side::Debit)
    {
        let pool_id = match line.target() {
            PostingTarget::Pool(id) => parse_uuid(id.as_str(), "pool_id")?,
            PostingTarget::Account(_) => {
                return Err(PostPurchasePersistenceError::InvalidDecision);
            }
        };
        let total = actual.entry(pool_id).or_insert(0);
        *total = total.checked_add(line.amount.value()).ok_or(
            PostPurchasePersistenceError::NumericOutOfRange("journal debit total"),
        )?;
    }
    if actual != expected {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    Ok(())
}
