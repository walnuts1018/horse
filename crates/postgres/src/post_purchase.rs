use std::collections::BTreeSet;

use bookkeeping_workflows::{PostPurchaseDecision, Side};
use chrono::{DateTime, Datelike, Utc};
use diesel::{
    ExpressionMethods, OptionalExtension, QueryDsl, dsl::now, result::Error as DieselError,
};
use diesel_async::{AsyncConnection, RunQueryDsl};
use uuid::Uuid;

use crate::{PgPool, schema::public};

const OPERATION: &str = "post_purchase";

#[derive(Debug, Clone)]
pub struct PersistPostPurchase {
    pub household_id: String,
    pub actor_id: String,
    pub expected_revision: u64,
    pub ledger_description: String,
    pub idempotency_key: String,
    pub request_fingerprint: [u8; 32],
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
    if input.idempotency_key.is_empty() || input.idempotency_key.len() > 200 {
        return Err(PostPurchasePersistenceError::InvalidIdempotencyKey);
    }
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    let purchase_id = parse_uuid(decision.purchase_id.as_str(), "purchase_id")?;
    let expected_revision = i64::try_from(input.expected_revision)
        .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("expected_revision"))?;
    let new_revision = i64::try_from(decision.new_revision)
        .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("new_revision"))?;
    let journal_id = Uuid::new_v4();
    let ledger_entry_id = Uuid::new_v4();
    validate_decision(&decision, expected_revision)?;

    let mut pooled = pool
        .get()
        .await
        .map_err(|error| PostPurchasePersistenceError::Pool(error.to_string()))?;
    let conn = &mut *pooled;
    conn.transaction::<PersistedPostPurchase, PostPurchasePersistenceError, _>(async |conn| {
        use public::idempotency_records::dsl as idem;
        use public::journals::dsl as journals;
        use public::ledger_entries::dsl as ledger;
        use public::purchases::dsl as purchases;

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
            let (fingerprint, resource_id) = idem::idempotency_records
                .filter(idem::household_id.eq(household_id))
                .filter(idem::operation.eq(OPERATION))
                .filter(idem::key.eq(&input.idempotency_key))
                .select((idem::request_fingerprint, idem::resource_id))
                .first::<(Vec<u8>, Uuid)>(&mut *conn)
                .await?;
            if fingerprint.as_slice() != input.request_fingerprint {
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
        let occurred_at = purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::id.eq(purchase_id))
            .select(purchases::occurred_at)
            .first::<DateTime<Utc>>(&mut *conn)
            .await?;
        let period_start = occurred_at
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

        for line in &decision.journal_lines {
            let line_no = i32::try_from(line.line_no)
                .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal line_no"))?;
            let amount = i64::try_from(line.amount.value())
                .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal amount"))?;
            diesel::insert_into(public::journal_entries::table)
                .values((
                    public::journal_entries::household_id.eq(household_id),
                    public::journal_entries::journal_id.eq(journal_id),
                    public::journal_entries::line_no.eq(line_no),
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
                    public::journal_entries::debit_yen.eq(if line.side == Side::Debit {
                        amount
                    } else {
                        0
                    }),
                    public::journal_entries::credit_yen.eq(if line.side == Side::Credit {
                        amount
                    } else {
                        0
                    }),
                    public::journal_entries::memo.eq(line.memo),
                ))
                .execute(&mut *conn)
                .await?;
        }

        for snapshot in &decision.member_burden_snapshots {
            let amount = snapshot.amount.value();
            let rule_version = i32::try_from(snapshot.funding_rule_version).map_err(|_| {
                PostPurchasePersistenceError::NumericOutOfRange("funding_rule_version")
            })?;
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
                ))
                .execute(&mut *conn)
                .await?;
        }

        for snapshot in &decision.payment_funding_snapshots {
            let line_no = i32::try_from(snapshot.line_no)
                .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("payment line_no"))?;
            let rule_version = i32::try_from(snapshot.funding_rule_version).map_err(|_| {
                PostPurchasePersistenceError::NumericOutOfRange("funding_rule_version")
            })?;
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
                    public::purchase_payment_funding_snapshots::funding_rule_version
                        .eq(rule_version),
                    public::purchase_payment_funding_snapshots::funding_weight.eq(weight),
                    public::purchase_payment_funding_snapshots::settlement_policy
                        .eq(snapshot.settlement_policy.as_deref()),
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

        for event in &decision.events {
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
    })
    .await
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, PostPurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| PostPurchasePersistenceError::InvalidUuid(field))
}

fn validate_decision(
    decision: &PostPurchaseDecision,
    expected_revision: i64,
) -> Result<(), PostPurchasePersistenceError> {
    if decision.new_status != bookkeeping_workflows::PurchaseStatus::Posted
        || decision.events.is_empty()
        || expected_revision.checked_add(1) != i64::try_from(decision.new_revision).ok()
    {
        return Err(PostPurchasePersistenceError::InvalidDecision);
    }
    for event in &decision.events {
        if event.entity_id != decision.purchase_id || event.revision != decision.new_revision {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
    }
    let mut lines = BTreeSet::new();
    let mut debit = 0_i64;
    let mut credit = 0_i64;
    for line in &decision.journal_lines {
        let number = i32::try_from(line.line_no)
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal line_no"))?;
        if number <= 0 || !lines.insert(number) {
            return Err(PostPurchasePersistenceError::InvalidDecision);
        }
        let amount = i64::try_from(line.amount.value())
            .map_err(|_| PostPurchasePersistenceError::NumericOutOfRange("journal amount"))?;
        match line.side {
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
    Ok(())
}

fn ledger_amount(decision: &PostPurchaseDecision) -> Result<i64, PostPurchasePersistenceError> {
    let total = decision
        .journal_lines
        .iter()
        .filter(|line| line.side == Side::Debit)
        .try_fold(0_i64, |sum, line| {
            sum.checked_add(line.amount.value()).ok_or(
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
) -> Result<(), PostPurchasePersistenceError> {
    use public::purchases::dsl as purchases;
    let changed = diesel::update(
        purchases::purchases
            .filter(purchases::household_id.eq(household_id))
            .filter(purchases::id.eq(purchase_id))
            .filter(purchases::revision.eq(expected_revision))
            .filter(purchases::status.eq("DRAFT")),
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
    for line in &decision.journal_lines {
        if let Some(id) = &line.pool_id {
            pool_ids.insert(parse_uuid(id.as_str(), "pool_id")?);
        }
        if let Some(id) = &line.account_id {
            account_ids.insert(parse_uuid(id.as_str(), "account_id")?);
        }
    }
    for snapshot in &decision.member_burden_snapshots {
        item_ids.insert(parse_uuid(snapshot.item_id.as_str(), "item_id")?);
        pool_ids.insert(parse_uuid(snapshot.pool_id.as_str(), "pool_id")?);
        member_ids.insert(parse_uuid(snapshot.member_id.as_str(), "member_id")?);
    }
    let mut account_pool_pairs = BTreeSet::new();
    for snapshot in &decision.payment_funding_snapshots {
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
