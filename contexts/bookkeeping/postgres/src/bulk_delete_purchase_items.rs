use std::collections::{BTreeMap, BTreeSet};

use bookkeeping_application::{
    MemberFundingBasis, MemberId, MemberWeight, PostingTarget, SettlementPolicy, Side,
    bulk_delete_items::{
        BulkDeleteItemCommand, BulkDeleteItemState, BulkDeleteItemTarget, BulkDeleteItemsError,
        BulkDeletePurchaseState, decide_bulk_delete_items,
    },
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
pub struct PersistBulkDeletePurchaseItems {
    pub household_id: String,
    pub actor_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PersistedBulkDeletePurchaseItems {
    pub deleted_item_ids: Vec<String>,
}

#[derive(Debug)]
pub enum BulkDeletePurchaseItemsPersistenceError {
    Pool(String),
    Database(DieselError),
    InvalidUuid(&'static str),
    NumericOutOfRange(&'static str),
    ItemNotFound,
    PurchaseNotFound,
    HouseholdMismatch,
    MemberNotFound,
    AccountingPeriodClosed,
    InvalidPaymentAllocation,
    InvalidFundingSnapshot,
    InvalidDecision,
    InvalidWorkflow(BulkDeleteItemsError),
}

impl From<DieselError> for BulkDeletePurchaseItemsPersistenceError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

#[derive(Clone)]
struct TargetRow {
    id: Uuid,
    purchase_id: Uuid,
    revision: i64,
    base_revision: u64,
}

pub async fn persist_bulk_delete_purchase_items(
    pool: &PgPool,
    input: PersistBulkDeletePurchaseItems,
    targets: Vec<BulkDeleteItemTarget>,
) -> Result<PersistedBulkDeletePurchaseItems, BulkDeletePurchaseItemsPersistenceError> {
    let household_id = parse_uuid(&input.household_id, "household_id")?;
    let actor_id = parse_uuid(&input.actor_id, "actor_id")?;
    if targets.is_empty() {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidWorkflow(
            BulkDeleteItemsError::EmptyTargets,
        ));
    }
    if targets.len() > 100 {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidWorkflow(
            BulkDeleteItemsError::TooManyTargets,
        ));
    }
    if let Some(error) = targets.iter().find_map(|target| {
        if target.item_id.is_empty() {
            Some(BulkDeleteItemsError::InvalidItemId)
        } else if target.base_revision == 0 {
            Some(BulkDeleteItemsError::InvalidRevision)
        } else {
            None
        }
    }) {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidWorkflow(
            error,
        ));
    }
    let target_ids = targets
        .iter()
        .map(|target| parse_uuid(&target.item_id, "item_id"))
        .collect::<Result<Vec<_>, _>>()?;
    if target_ids.iter().copied().collect::<BTreeSet<_>>().len() != target_ids.len() {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidWorkflow(
            BulkDeleteItemsError::DuplicateTarget,
        ));
    }
    let target_revisions = target_ids
        .iter()
        .copied()
        .zip(targets.iter().map(|target| target.base_revision))
        .collect::<BTreeMap<_, _>>();
    let command_targets = target_ids
        .iter()
        .zip(&targets)
        .map(|(id, target)| BulkDeleteItemTarget {
            item_id: id.to_string(),
            base_revision: target.base_revision,
        })
        .collect::<Vec<_>>();
    let mut pooled = pool
        .get()
        .await
        .map_err(|error| BulkDeletePurchaseItemsPersistenceError::Pool(error.to_string()))?;

    (&mut *pooled)
        .transaction::<PersistedBulkDeletePurchaseItems, BulkDeletePurchaseItemsPersistenceError, _>(
            async move |conn| {
                use public::accounting_periods::dsl as periods;
                use public::household_events::dsl as events;
                use public::household_members::dsl as members;
                use public::households::dsl as households;
                use public::purchase_items::dsl as items;
                use public::purchases::dsl as purchases;

                let observations = items::purchase_items
                    .filter(items::id.eq_any(&target_ids))
                    .order(items::id.asc())
                    .select((items::id, items::household_id, items::purchase_id, items::revision))
                    .load::<(Uuid, Uuid, Uuid, i64)>(conn)
                    .await?;
                if observations.len() != target_ids.len() {
                    return Err(BulkDeletePurchaseItemsPersistenceError::ItemNotFound);
                }
                let mut target_rows = observations
                    .into_iter()
                    .map(|(id, owner, purchase_id, revision)| {
                        if owner != household_id {
                            return Err(BulkDeletePurchaseItemsPersistenceError::HouseholdMismatch);
                        }
                        let base_revision = target_revisions
                            .get(&id)
                            .copied()
                            .ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidDecision)?;
                        Ok(TargetRow { id, purchase_id, revision, base_revision })
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                let purchase_ids = target_rows.iter().map(|row| row.purchase_id).collect::<BTreeSet<_>>();
                let observed_purchases = purchases::purchases
                    .filter(purchases::id.eq_any(purchase_ids.iter().copied()))
                    .order(purchases::id.asc())
                    .select((purchases::id, purchases::household_id, purchases::occurred_at, purchases::status, purchases::revision, purchases::merchant, purchases::payment_account_id))
                    .load::<(Uuid, Uuid, DateTime<Utc>, String, i64, String, Option<Uuid>)>(conn)
                    .await?;
                if observed_purchases.len() != purchase_ids.len() {
                    return Err(BulkDeletePurchaseItemsPersistenceError::PurchaseNotFound);
                }
                if observed_purchases.iter().any(|purchase| purchase.1 != household_id) {
                    return Err(BulkDeletePurchaseItemsPersistenceError::HouseholdMismatch);
                }
                let periods_to_lock = observed_purchases
                    .iter()
                    .map(|purchase| period_start(purchase.2))
                    .collect::<Result<BTreeSet<_>, _>>()?;
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
                    return Err(BulkDeletePurchaseItemsPersistenceError::MemberNotFound);
                }
                let period_dates = periods_to_lock.iter().copied().collect::<Vec<_>>();
                for period in &period_dates {
                    diesel::insert_into(periods::accounting_periods)
                        .values((periods::household_id.eq(household_id), periods::period_start.eq(period), periods::status.eq("OPEN"), periods::revision.eq(1_i64)))
                        .on_conflict((periods::household_id, periods::period_start))
                        .do_nothing()
                        .execute(&mut *conn)
                        .await?;
                }
                let locked_periods = periods::accounting_periods
                    .filter(periods::household_id.eq(household_id))
                    .filter(periods::period_start.eq_any(&period_dates))
                    .order(periods::period_start.asc())
                    .for_update()
                    .select((periods::period_start, periods::status))
                    .load::<(NaiveDate, String)>(&mut *conn)
                    .await?;
                if locked_periods.len() != period_dates.len() || locked_periods.iter().any(|(_, status)| status != "OPEN") {
                    return Err(BulkDeletePurchaseItemsPersistenceError::AccountingPeriodClosed);
                }
                let _household = households::households
                    .filter(households::id.eq(household_id))
                    .for_share()
                    .select(households::id)
                    .first::<Uuid>(&mut *conn)
                    .await
                    .optional()?
                    .ok_or(BulkDeletePurchaseItemsPersistenceError::HouseholdMismatch)?;

                let mut locked_purchases = BTreeMap::new();
                for (id, _, occurred_at, _, _, _, _) in &observed_purchases {
                    let locked = purchases::purchases
                        .filter(purchases::id.eq(id))
                        .for_update()
                        .select((purchases::household_id, purchases::occurred_at, purchases::status, purchases::revision, purchases::merchant, purchases::payment_account_id))
                        .first::<(Uuid, DateTime<Utc>, String, i64, String, Option<Uuid>)>(&mut *conn)
                        .await
                        .optional()?
                        .ok_or(BulkDeletePurchaseItemsPersistenceError::PurchaseNotFound)?;
                    if locked.0 != household_id || locked.1 != *occurred_at {
                        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidDecision);
                    }
                    if !matches!(locked.2.as_str(), "DRAFT" | "POSTED") {
                        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidDecision);
                    }
                    locked_purchases.insert(*id, (locked.1, locked.2, locked.3, locked.4, locked.5));
                }
                target_rows.sort_by_key(|row| row.id);
                for row in &mut target_rows {
                    let locked = items::purchase_items
                        .filter(items::id.eq(row.id))
                        .for_update()
                        .select((items::household_id, items::purchase_id, items::revision))
                        .first::<(Uuid, Uuid, i64)>(&mut *conn)
                        .await
                        .optional()?
                        .ok_or(BulkDeletePurchaseItemsPersistenceError::ItemNotFound)?;
                    if locked.0 != household_id || locked.1 != row.purchase_id {
                        return Err(BulkDeletePurchaseItemsPersistenceError::HouseholdMismatch);
                    }
                    if locked.2 != row.revision || u64::try_from(locked.2).ok() != Some(row.base_revision) {
                        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidWorkflow(BulkDeleteItemsError::Conflict));
                    }
                    row.revision = locked.2;
                }
                let mut purchase_states = Vec::with_capacity(purchase_ids.len());
                for purchase_id in &purchase_ids {
                    let (_, _, revision, _, _) = locked_purchases.get(purchase_id).ok_or(BulkDeletePurchaseItemsPersistenceError::PurchaseNotFound)?;
                    let rows = items::purchase_items
                        .filter(items::household_id.eq(household_id))
                        .filter(items::purchase_id.eq(purchase_id))
                        .order(items::id.asc())
                        .select((items::id, items::revision, items::amount_yen))
                        .load::<(Uuid, i64, i64)>(&mut *conn)
                        .await?;
                    let mut states = Vec::with_capacity(rows.len());
                    for (id, revision, amount_yen) in &rows {
                        let corrected = public::purchase_corrections::dsl::purchase_corrections
                            .filter(public::purchase_corrections::dsl::household_id.eq(household_id))
                            .filter(public::purchase_corrections::dsl::purchase_id.eq(purchase_id))
                            .filter(public::purchase_corrections::dsl::item_id.eq(Some(*id)))
                            .select(public::purchase_corrections::dsl::id)
                            .first::<Uuid>(&mut *conn)
                            .await
                            .optional()?
                            .is_some();
                        states.push(BulkDeleteItemState {
                            item_id: id.to_string(),
                            revision: u64::try_from(*revision).map_err(|_| BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("item_revision"))?,
                            amount_yen: *amount_yen,
                            corrected,
                        });
                    }
                    purchase_states.push(BulkDeletePurchaseState {
                        purchase_id: purchase_id.to_string(),
                        revision: u64::try_from(*revision).map_err(|_| BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("purchase_revision"))?,
                        item_count: rows.len(),
                        items: states,
                    });
                }
                let decisions = decide_bulk_delete_items(
                    BulkDeleteItemCommand { targets: command_targets },
                    purchase_states,
                ).map_err(BulkDeletePurchaseItemsPersistenceError::InvalidWorkflow)?;
                let deleted_ids = decisions.iter().flat_map(|decision| decision.deleted_item_ids.iter().cloned()).collect::<Vec<_>>();

                let mut previous_accounting = BTreeMap::new();
                for decision in &decisions {
                    let purchase_id = parse_uuid(&decision.purchase_id, "purchase_id")?;
                    let purchase = locked_purchases.get(&purchase_id).ok_or(BulkDeletePurchaseItemsPersistenceError::PurchaseNotFound)?;
                    let deleted_amount = decision.deleted_amount_yen;
                    let amounts = items::purchase_items
                        .filter(items::household_id.eq(household_id))
                        .filter(items::purchase_id.eq(purchase_id))
                        .select(items::amount_yen)
                        .load::<i64>(&mut *conn)
                        .await?;
                    let old_total = checked_sum(amounts)?;
                    let new_total = old_total.checked_sub(deleted_amount).filter(|amount| *amount >= 0).ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation)?;
                    let payment_rows = public::purchase_payments::dsl::purchase_payments
                        .filter(public::purchase_payments::dsl::household_id.eq(household_id))
                        .filter(public::purchase_payments::dsl::purchase_id.eq(purchase_id))
                        .order(public::purchase_payments::dsl::line_no.asc())
                        .select((public::purchase_payments::dsl::account_id, public::purchase_payments::dsl::amount_yen))
                        .load::<(Uuid, i64)>(&mut *conn)
                        .await?;
                    let effective_payments = if payment_rows.is_empty() {
                        purchase.4.map(|account_id| vec![(account_id, old_total)]).unwrap_or_default()
                    } else {
                        payment_rows.clone()
                    };
                    let adjusted_payments = if deleted_amount == 0 {
                        effective_payments.clone()
                    } else {
                        apportion_payments(&effective_payments, old_total, new_total)?
                    };
                    if deleted_amount != 0 && !effective_payments.is_empty() {
                        diesel::delete(public::purchase_payments::dsl::purchase_payments
                            .filter(public::purchase_payments::dsl::household_id.eq(household_id))
                            .filter(public::purchase_payments::dsl::purchase_id.eq(purchase_id)))
                            .execute(&mut *conn).await?;
                        for (index, (account_id, amount_yen)) in adjusted_payments.iter().enumerate() {
                            diesel::insert_into(public::purchase_payments::table)
                                .values((
                                    public::purchase_payments::dsl::household_id.eq(household_id),
                                    public::purchase_payments::dsl::purchase_id.eq(purchase_id),
                                    public::purchase_payments::dsl::line_no.eq(i32::try_from(index + 1).map_err(|_| BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("payment_line_no"))?),
                                    public::purchase_payments::dsl::account_id.eq(account_id),
                                    public::purchase_payments::dsl::amount_yen.eq(amount_yen),
                                )).execute(&mut *conn).await?;
                        }
                    }
                    previous_accounting.insert(purchase_id, (purchase.0, purchase.1.clone(), purchase.2, purchase.3.clone(), effective_payments, adjusted_payments));
                }
                for decision in &decisions {
                    let purchase_id = parse_uuid(&decision.purchase_id, "purchase_id")?;
                    let deleted = decision.deleted_item_ids.iter().map(|id| parse_uuid(id, "item_id")).collect::<Result<Vec<_>, _>>()?;
                    for item_id in &deleted {
                        let revision = target_rows.iter().find(|row| row.id == *item_id)
                            .ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidDecision)?
                            .revision.checked_add(1)
                            .ok_or(BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("item_revision"))?;
                        diesel::delete(items::purchase_items.filter(items::household_id.eq(household_id)).filter(items::purchase_id.eq(purchase_id)).filter(items::id.eq(item_id)))
                            .execute(&mut *conn).await?;
                        diesel::insert_into(events::household_events)
                            .values((
                                events::household_id.eq(household_id),
                                events::event_type.eq("purchase.item.deleted"),
                                events::entity_kind.eq("purchase_item"),
                                events::entity_id.eq(item_id),
                                events::revision.eq(revision),
                                events::changed_fields.eq(vec![Some("deleted".to_owned())]),
                                events::payload.eq(serde_json::json!({
                                    "purchaseId": purchase_id.to_string(),
                                })),
                                events::actor_id.eq(actor_id),
                            )).execute(&mut *conn).await?;
                    }
                    let next_revision = i64::try_from(decision.purchase_revision).map_err(|_| BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("purchase_revision"))?;
                    diesel::update(purchases::purchases.filter(purchases::id.eq(purchase_id)))
                        .set((purchases::revision.eq(next_revision), purchases::updated_at.eq(now)))
                        .execute(&mut *conn).await?;
                    let (_, status, old_revision, _, old_payments, new_payments) = previous_accounting.get(&purchase_id).ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidDecision)?;
                    if status == "POSTED" {
                        sync_posted_accounting(&mut *conn, household_id, purchase_id, *old_revision, *old_payments != *new_payments).await?;
                    }
                }
                Ok(PersistedBulkDeletePurchaseItems { deleted_item_ids: deleted_ids })
            },
        )
        .await
}

async fn sync_posted_accounting(
    conn: &mut crate::AsyncPgConnection,
    household_id: Uuid,
    purchase_id: Uuid,
    revision: i64,
    payments_changed: bool,
) -> Result<(), BulkDeletePurchaseItemsPersistenceError> {
    use public::purchase_member_burdens::dsl as burdens;
    use public::purchase_payment_funding_snapshots::dsl as snapshots;
    let old_burdens = burdens::purchase_member_burdens
        .filter(burdens::household_id.eq(household_id))
        .filter(burdens::purchase_id.eq(purchase_id))
        .select((
            burdens::item_id,
            burdens::pool_id,
            burdens::member_id,
            burdens::funding_rule_version,
            burdens::funding_weight,
            burdens::settlement_policy,
            burdens::snapshot_source,
        ))
        .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(&mut *conn)
        .await?;
    let old_payments = snapshots::purchase_payment_funding_snapshots
        .filter(snapshots::household_id.eq(household_id))
        .filter(snapshots::purchase_id.eq(purchase_id))
        .select((
            snapshots::account_id,
            snapshots::pool_id,
            snapshots::member_id,
            snapshots::funding_rule_version,
            snapshots::funding_weight,
            snapshots::settlement_policy,
            snapshots::snapshot_source,
        ))
        .load::<(Uuid, Uuid, Uuid, i32, i64, Option<String>, String)>(&mut *conn)
        .await?;
    let burden_sources = old_burdens
        .iter()
        .map(|(item, pool, member, _, _, _, source)| ((*item, *pool, *member), source.clone()))
        .collect::<BTreeMap<_, _>>();
    let payment_sources = old_payments
        .iter()
        .map(|(account, pool, member, _, _, _, source)| {
            ((*account, *pool, *member), source.clone())
        })
        .collect::<BTreeMap<_, _>>();
    let burden_bases = collect_bases(old_burdens.iter().map(
        |(item, pool, member, version, weight, policy, _)| {
            ((*item, *pool), (*member, *version, *weight, policy.clone()))
        },
    ))?;
    let payment_bases = collect_bases(old_payments.iter().map(
        |(account, pool, member, version, weight, policy, _)| {
            (
                (*account, *pool),
                (*member, *version, *weight, policy.clone()),
            )
        },
    ))?;
    let accounts = public::purchase_payments::dsl::purchase_payments
        .filter(public::purchase_payments::dsl::household_id.eq(household_id))
        .filter(public::purchase_payments::dsl::purchase_id.eq(purchase_id))
        .select(public::purchase_payments::dsl::account_id)
        .load::<Uuid>(&mut *conn)
        .await?
        .into_iter()
        .collect::<BTreeSet<_>>();
    let loaded = crate::post_purchase_loader::load_post_purchase_command(
        conn,
        &household_id.to_string(),
        &purchase_id.to_string(),
        u64::try_from(revision).map_err(|_| {
            BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("purchase_revision")
        })?,
    )
    .await
    .map_err(|error| match error {
        crate::post_purchase_loader::PostPurchaseLoadError::Database(error) => {
            BulkDeletePurchaseItemsPersistenceError::Database(error)
        }
        _ => BulkDeletePurchaseItemsPersistenceError::InvalidDecision,
    })?;
    let mut command = loaded.command;
    command.expected_revision = command.draft.revision;
    for allocation in &mut command.draft.allocations {
        let key = (
            parse_uuid(allocation.item_id.as_str(), "item_id")?,
            parse_uuid(allocation.pool_id.as_str(), "pool_id")?,
        );
        if let Some(basis) = burden_bases.get(&key) {
            allocation.funding = basis.clone();
        }
    }
    for payment in &mut command.draft.payments {
        let account = parse_uuid(payment.account_id.as_str(), "account_id")?;
        let pool = parse_uuid(payment.funding.pool_id.as_str(), "pool_id")?;
        if let Some(basis) = payment_bases.get(&(account, pool)).or_else(|| {
            payment_bases
                .iter()
                .find(|((_, old_pool), _)| *old_pool == pool)
                .map(|(_, basis)| basis)
        }) {
            payment.funding.rule = basis.clone();
        } else if payments_changed && accounts.contains(&account) {
            return Err(BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot);
        }
    }
    let decision = decide_post_purchase(command)
        .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidDecision)?;
    let journal_ids = public::journals::dsl::journals
        .filter(public::journals::dsl::household_id.eq(household_id))
        .filter(public::journals::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
        .filter(public::journals::dsl::source_id.eq(purchase_id))
        .select(public::journals::dsl::id)
        .load::<Uuid>(&mut *conn)
        .await?;
    if journal_ids.len() != 1 {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidDecision);
    }
    let journal_id = journal_ids[0];
    diesel::delete(
        public::journal_entries::dsl::journal_entries
            .filter(public::journal_entries::dsl::household_id.eq(household_id))
            .filter(public::journal_entries::dsl::journal_id.eq(journal_id)),
    )
    .execute(&mut *conn)
    .await?;
    for line in decision.journal_lines() {
        let (account_id, pool_id) = match line.target() {
            PostingTarget::Account(id) => (Some(parse_uuid(id.as_str(), "account_id")?), None),
            PostingTarget::Pool(id) => (None, Some(parse_uuid(id.as_str(), "pool_id")?)),
        };
        diesel::insert_into(public::journal_entries::table)
            .values((
                public::journal_entries::dsl::household_id.eq(household_id),
                public::journal_entries::dsl::journal_id.eq(journal_id),
                public::journal_entries::dsl::line_no.eq(i32::try_from(line.line_no()).map_err(
                    |_| {
                        BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange(
                            "journal_line_no",
                        )
                    },
                )?),
                public::journal_entries::dsl::account_id.eq(account_id),
                public::journal_entries::dsl::pool_id.eq(pool_id),
                public::journal_entries::dsl::member_id.eq::<Option<Uuid>>(None),
                public::journal_entries::dsl::debit_yen.eq(if line.side() == Side::Debit {
                    line.amount().value()
                } else {
                    0
                }),
                public::journal_entries::dsl::credit_yen.eq(if line.side() == Side::Credit {
                    line.amount().value()
                } else {
                    0
                }),
                public::journal_entries::dsl::memo.eq(line.memo()),
            ))
            .execute(&mut *conn)
            .await?;
    }
    diesel::delete(
        burdens::purchase_member_burdens
            .filter(burdens::household_id.eq(household_id))
            .filter(burdens::purchase_id.eq(purchase_id)),
    )
    .execute(&mut *conn)
    .await?;
    for snapshot in decision.member_burden_snapshots() {
        let item = parse_uuid(snapshot.item_id.as_str(), "item_id")?;
        let pool = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
        let member = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
        diesel::insert_into(burdens::purchase_member_burdens)
            .values((
                burdens::household_id.eq(household_id),
                burdens::purchase_id.eq(purchase_id),
                burdens::item_id.eq(item),
                burdens::pool_id.eq(pool),
                burdens::member_id.eq(member),
                burdens::amount_yen.eq(snapshot.amount.value()),
                burdens::funding_rule_version.eq(i32::try_from(snapshot.funding_rule_version)
                    .map_err(|_| {
                        BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange(
                            "funding_rule_version",
                        )
                    })?),
                burdens::funding_weight.eq(i64::try_from(snapshot.funding_weight).map_err(
                    |_| {
                        BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange("funding_weight")
                    },
                )?),
                burdens::settlement_policy
                    .eq(snapshot.settlement_policy.map(|policy| policy.as_str())),
                burdens::snapshot_source.eq(burden_sources
                    .get(&(item, pool, member))
                    .map(String::as_str)
                    .unwrap_or("POSTED")),
            ))
            .execute(&mut *conn)
            .await?;
    }
    if payments_changed {
        diesel::delete(
            snapshots::purchase_payment_funding_snapshots
                .filter(snapshots::household_id.eq(household_id))
                .filter(snapshots::purchase_id.eq(purchase_id)),
        )
        .execute(&mut *conn)
        .await?;
        for snapshot in decision.payment_funding_snapshots() {
            let account = parse_uuid(snapshot.account_id.as_str(), "account_id")?;
            let pool = parse_uuid(snapshot.pool_id.as_str(), "pool_id")?;
            let member = parse_uuid(snapshot.member_id.as_str(), "member_id")?;
            diesel::insert_into(snapshots::purchase_payment_funding_snapshots)
                .values((
                    snapshots::household_id.eq(household_id),
                    snapshots::purchase_id.eq(purchase_id),
                    snapshots::payment_line_no.eq(i32::try_from(snapshot.line_no).map_err(
                        |_| {
                            BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange(
                                "payment_line_no",
                            )
                        },
                    )?),
                    snapshots::account_id.eq(account),
                    snapshots::pool_id.eq(pool),
                    snapshots::member_id.eq(member),
                    snapshots::funding_rule_version.eq(i32::try_from(
                        snapshot.funding_rule_version,
                    )
                    .map_err(|_| {
                        BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange(
                            "funding_rule_version",
                        )
                    })?),
                    snapshots::funding_weight.eq(i64::try_from(snapshot.funding_weight).map_err(
                        |_| {
                            BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange(
                                "funding_weight",
                            )
                        },
                    )?),
                    snapshots::settlement_policy
                        .eq(snapshot.settlement_policy.map(|policy| policy.as_str())),
                    snapshots::snapshot_source.eq(payment_sources
                        .get(&(account, pool, member))
                        .or_else(|| {
                            payment_sources
                                .iter()
                                .find(|((_, old_pool, old_member), _)| {
                                    *old_pool == pool && *old_member == member
                                })
                                .map(|(_, source)| source)
                        })
                        .map(String::as_str)
                        .unwrap_or("POSTED")),
                ))
                .execute(&mut *conn)
                .await?;
        }
    }
    let amount = decision
        .journal_lines()
        .iter()
        .filter(|line| line.side() == Side::Debit)
        .try_fold(0_i64, |total, line| {
            total.checked_add(line.amount().value())
        })
        .ok_or(BulkDeletePurchaseItemsPersistenceError::NumericOutOfRange(
            "ledger_amount",
        ))?;
    let updated = diesel::update(
        public::ledger_entries::dsl::ledger_entries
            .filter(public::ledger_entries::dsl::household_id.eq(household_id))
            .filter(public::ledger_entries::dsl::source_kind.eq_any(["purchase", "PURCHASE"]))
            .filter(public::ledger_entries::dsl::source_id.eq(purchase_id)),
    )
    .set(public::ledger_entries::dsl::amount_yen.eq(amount))
    .execute(&mut *conn)
    .await?;
    if updated == 0 {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidDecision);
    }
    Ok(())
}

fn collect_bases<K: Ord + Copy>(
    rows: impl Iterator<Item = (K, (Uuid, i32, i64, Option<String>))>,
) -> Result<BTreeMap<K, MemberFundingBasis>, BulkDeletePurchaseItemsPersistenceError> {
    let mut groups = BTreeMap::<K, Vec<(Uuid, i32, i64, Option<String>)>>::new();
    for (key, row) in rows {
        groups.entry(key).or_default().push(row);
    }
    groups
        .into_iter()
        .map(|(key, rows)| Ok((key, funding_basis(rows)?)))
        .collect()
}

fn funding_basis(
    rows: Vec<(Uuid, i32, i64, Option<String>)>,
) -> Result<MemberFundingBasis, BulkDeletePurchaseItemsPersistenceError> {
    let Some((_, version, _, policy)) = rows.as_slice().first() else {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot);
    };
    if rows.iter().any(|(_, other_version, _, other_policy)| {
        other_version != version || other_policy != policy
    }) {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot);
    }
    let version = u32::try_from(*version)
        .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot)?;
    if let Some(policy) = policy {
        let policy = match policy.as_str() {
            "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
            "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
            _ => return Err(BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot),
        };
        let weights = rows
            .into_iter()
            .map(|(member, _, weight, _)| {
                MemberWeight::new(
                    MemberId::new(member.to_string()).map_err(|_| {
                        BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot
                    })?,
                    u64::try_from(weight).map_err(|_| {
                        BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot
                    })?,
                )
                .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot)
            })
            .collect::<Result<Vec<_>, _>>()?;
        MemberFundingBasis::shared(version, weights, policy)
            .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot)
    } else {
        if rows.len() != 1 || rows[0].2 != 1 {
            return Err(BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot);
        }
        MemberFundingBasis::personal(
            version,
            MemberId::new(rows[0].0.to_string())
                .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot)?,
        )
        .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidFundingSnapshot)
    }
}

fn apportion_payments(
    rows: &[(Uuid, i64)],
    old_total: i64,
    new_total: i64,
) -> Result<Vec<(Uuid, i64)>, BulkDeletePurchaseItemsPersistenceError> {
    if rows.is_empty() {
        return Ok(Vec::new());
    }
    let paid_total = checked_sum(rows.iter().map(|(_, amount)| *amount))?;
    if paid_total != old_total || rows.iter().any(|(_, amount)| *amount <= 0) || old_total <= 0 {
        return Err(BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation);
    }
    let denominator = u128::try_from(paid_total)
        .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation)?;
    let target = u128::try_from(new_total)
        .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation)?;
    let mut parts = rows
        .iter()
        .enumerate()
        .map(|(index, (account, amount))| {
            let product = target * (*amount as u128);
            (
                *account,
                (product / denominator) as i64,
                product % denominator,
                index,
            )
        })
        .collect::<Vec<_>>();
    let assigned = parts
        .iter()
        .try_fold(0_i64, |sum, row| sum.checked_add(row.1))
        .ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation)?;
    let remainder = usize::try_from(new_total - assigned)
        .map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation)?;
    let mut priority = (0..parts.len()).collect::<Vec<_>>();
    priority.sort_by(|left, right| {
        parts[*right]
            .2
            .cmp(&parts[*left].2)
            .then_with(|| parts[*left].0.cmp(&parts[*right].0))
    });
    for index in priority.into_iter().take(remainder) {
        parts[index].1 += 1;
    }
    Ok(parts
        .into_iter()
        .filter_map(|(account, amount, _, _)| (amount > 0).then_some((account, amount)))
        .collect())
}

fn checked_sum(
    values: impl IntoIterator<Item = i64>,
) -> Result<i64, BulkDeletePurchaseItemsPersistenceError> {
    values
        .into_iter()
        .try_fold(0_i64, |sum, value| sum.checked_add(value))
        .ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidPaymentAllocation)
}

fn period_start(
    value: DateTime<Utc>,
) -> Result<NaiveDate, BulkDeletePurchaseItemsPersistenceError> {
    let tokyo = FixedOffset::east_opt(9 * 60 * 60)
        .ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidDecision)?;
    let date = value.with_timezone(&tokyo);
    NaiveDate::from_ymd_opt(date.year(), date.month(), 1)
        .ok_or(BulkDeletePurchaseItemsPersistenceError::InvalidDecision)
}

fn parse_uuid(
    value: &str,
    field: &'static str,
) -> Result<Uuid, BulkDeletePurchaseItemsPersistenceError> {
    Uuid::parse_str(value).map_err(|_| BulkDeletePurchaseItemsPersistenceError::InvalidUuid(field))
}
