use std::collections::{BTreeMap, BTreeSet};
use std::future::Future;

use bookkeeping_application::{
    AccountId, AccountingPeriod, AccountingPeriodStatus, DraftPurchase, FundingBasisError, ItemId,
    MemberFundingBasis, MemberId, MemberWeight, Payment, PaymentFundingBasis, PoolAllocation,
    PostPurchaseCommand, PurchaseId, PurchaseItem, SettlementPolicy, YearMonth,
};
use bookkeeping_domain::{DraftPurchaseReconstitutionError, PoolId, PositiveYen, Yen};
use chrono::{DateTime, Datelike, FixedOffset, NaiveDate, Utc};
use diesel::{ExpressionMethods, OptionalExtension, QueryDsl, result::Error as DieselError};
use diesel_async::{AsyncPgConnection, RunQueryDsl};
use uuid::Uuid;

use crate::schema::public;

pub trait PostPurchaseReferenceData: Send {
    fn load_funding_rule(
        &mut self,
        conn: &mut AsyncPgConnection,
        household_id: Uuid,
        pool_id: Uuid,
        version: i32,
    ) -> impl Future<
        Output = Result<Option<(Option<Uuid>, Option<String>, Vec<(Uuid, i64)>)>, DieselError>,
    > + Send;
}

#[derive(Debug)]
pub enum PostPurchaseLoadError {
    Database(DieselError),
    InvalidUuid(&'static str),
    InvalidIdentifier(&'static str),
    NumericOutOfRange(&'static str),
    InvalidAmount(&'static str),
    InvalidEnum { field: &'static str, value: String },
    MissingPurchase,
    MissingRow(&'static str),
    HouseholdMismatch(&'static str),
    MissingAccountPool,
    MissingFundingRule,
    IncompleteFundingRule(&'static str),
    InvalidPaymentLineNumber,
    InvalidFundingBasis(FundingBasisError),
    InvalidDraftPurchase(DraftPurchaseReconstitutionError),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResolvedPostPurchasePayment {
    pub line_no: u32,
    pub account_id: AccountId,
    pub amount: PositiveYen,
    pub is_fallback: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct LoadedPostPurchaseCommand {
    pub command: PostPurchaseCommand,
    pub resolved_payments: Vec<ResolvedPostPurchasePayment>,
}

impl From<DieselError> for PostPurchaseLoadError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

/// `purchase_id`のDraft状態からworkflow commandを構築する。呼び出し側が開始したtransaction内で実行する。
pub async fn load_post_purchase_command<C: PostPurchaseReferenceData>(
    conn: &mut AsyncPgConnection,
    household_id: &str,
    purchase_id: &str,
    expected_revision: u64,
    reference_data: &mut C,
) -> Result<LoadedPostPurchaseCommand, PostPurchaseLoadError> {
    use public::accounting_periods::dsl as periods;
    use public::accounts::dsl as accounts;
    use public::household_members::dsl as household_members;
    use public::pools::dsl as pools;
    use public::purchase_allocations::dsl as allocations;
    use public::purchase_items::dsl as items;
    use public::purchase_payments::dsl as payments;
    use public::purchases::dsl as purchases;

    let household_id = parse_uuid(household_id, "household_id")?;
    let purchase_id = parse_uuid(purchase_id, "purchase_id")?;

    let period_hint = purchases::purchases
        .filter(purchases::id.eq(purchase_id))
        .select((purchases::household_id, purchases::occurred_at))
        .first::<(Uuid, DateTime<Utc>)>(conn)
        .await
        .optional()?
        .ok_or(PostPurchaseLoadError::MissingPurchase)?;
    if period_hint.0 != household_id {
        return Err(PostPurchaseLoadError::HouseholdMismatch("purchase"));
    }
    let tokyo = FixedOffset::east_opt(9 * 60 * 60).ok_or(PostPurchaseLoadError::InvalidEnum {
        field: "accounting timezone",
        value: "+09:00".to_owned(),
    })?;
    let occurred_at = period_hint.1.with_timezone(&tokyo);
    let period_start = NaiveDate::from_ymd_opt(occurred_at.year(), occurred_at.month(), 1).ok_or(
        PostPurchaseLoadError::InvalidEnum {
            field: "purchases.occurred_at",
            value: occurred_at.to_string(),
        },
    )?;
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
    let (period_status, period_revision) = periods::accounting_periods
        .filter(periods::household_id.eq(household_id))
        .filter(periods::period_start.eq(period_start))
        .for_update()
        .select((periods::status, periods::revision))
        .first::<(String, i64)>(conn)
        .await?;
    let period_status = match period_status.as_str() {
        "OPEN" => AccountingPeriodStatus::Open,
        "CLOSED" => AccountingPeriodStatus::Closed,
        value => {
            return Err(PostPurchaseLoadError::InvalidEnum {
                field: "accounting_periods.status",
                value: value.to_owned(),
            });
        }
    };
    let period_revision = u64::try_from(period_revision)
        .map_err(|_| PostPurchaseLoadError::NumericOutOfRange("accounting_period.revision"))?;
    let year_month = YearMonth::new(
        u16::try_from(period_start.year()).map_err(|_| PostPurchaseLoadError::InvalidEnum {
            field: "accounting_period.year",
            value: period_start.year().to_string(),
        })?,
        u8::try_from(period_start.month()).map_err(|_| PostPurchaseLoadError::InvalidEnum {
            field: "accounting_period.month",
            value: period_start.month().to_string(),
        })?,
    )
    .map_err(|_| PostPurchaseLoadError::InvalidEnum {
        field: "accounting_period.period_start",
        value: period_start.to_string(),
    })?;
    let period = match period_status {
        AccountingPeriodStatus::Open => AccountingPeriod::open(year_month, period_revision),
        AccountingPeriodStatus::Closed => AccountingPeriod::closed(year_month, period_revision),
    }
    .map_err(|_| PostPurchaseLoadError::InvalidEnum {
        field: "accounting_period.revision",
        value: period_revision.to_string(),
    })?;

    let purchase = purchases::purchases
        .filter(purchases::id.eq(purchase_id))
        .for_update()
        .select((
            purchases::household_id,
            purchases::occurred_at,
            purchases::status,
            purchases::revision,
            purchases::payment_account_id,
        ))
        .first::<(Uuid, DateTime<Utc>, String, i64, Option<Uuid>)>(conn)
        .await
        .optional()?
        .ok_or(PostPurchaseLoadError::MissingPurchase)?;
    if purchase.0 != household_id || purchase.1 != period_hint.1 {
        return Err(PostPurchaseLoadError::HouseholdMismatch("purchase"));
    }
    let revision = u64::try_from(purchase.3)
        .map_err(|_| PostPurchaseLoadError::NumericOutOfRange("purchase.revision"))?;
    if purchase.2 != "DRAFT" {
        return Err(PostPurchaseLoadError::InvalidEnum {
            field: "purchases.status",
            value: purchase.2,
        });
    }
    let draft_id = PurchaseId::new(purchase_id.to_string())
        .map_err(|_| PostPurchaseLoadError::InvalidIdentifier("purchase_id"))?;

    let item_rows = items::purchase_items
        .filter(items::purchase_id.eq(purchase_id))
        .order_by(items::id.asc())
        .for_update()
        .select((items::household_id, items::id, items::amount_yen))
        .load::<(Uuid, Uuid, i64)>(conn)
        .await?;
    let mut purchase_items = Vec::with_capacity(item_rows.len());
    let mut total = Yen::new(0).expect("zero is within Yen range");
    for (row_household_id, id, amount_yen) in item_rows {
        if row_household_id != household_id {
            return Err(PostPurchaseLoadError::HouseholdMismatch("purchase_items"));
        }
        let amount = yen(amount_yen, "purchase_items.amount_yen")?;
        total = total
            .checked_add(amount)
            .map_err(|_| PostPurchaseLoadError::InvalidAmount("purchase total"))?;
        purchase_items.push(PurchaseItem {
            id: ItemId::new(id.to_string())
                .map_err(|_| PostPurchaseLoadError::InvalidIdentifier("purchase_items.id"))?,
            amount,
        });
    }
    let total = PositiveYen::new(total.value())
        .map_err(|_| PostPurchaseLoadError::InvalidAmount("purchase total"))?;

    let allocation_rows = allocations::purchase_allocations
        .filter(allocations::purchase_id.eq(purchase_id))
        .order_by((allocations::item_id.asc(), allocations::pool_id.asc()))
        .for_update()
        .select((
            allocations::household_id,
            allocations::item_id,
            allocations::pool_id,
            allocations::amount_yen,
        ))
        .load::<(Uuid, Uuid, Uuid, i64)>(conn)
        .await?;
    let mut raw_allocations = Vec::with_capacity(allocation_rows.len());
    let mut pool_ids = BTreeSet::new();
    let item_ids = purchase_items
        .iter()
        .map(|item| item.id.as_str().to_owned())
        .collect::<BTreeSet<_>>();
    for (row_household_id, item_id, pool_id, amount_yen) in allocation_rows {
        if row_household_id != household_id {
            return Err(PostPurchaseLoadError::HouseholdMismatch(
                "purchase_allocations",
            ));
        }
        if !item_ids.contains(&item_id.to_string()) {
            return Err(PostPurchaseLoadError::MissingRow(
                "purchase allocation item",
            ));
        }
        pool_ids.insert(pool_id);
        raw_allocations.push((
            item_id,
            pool_id,
            yen(amount_yen, "purchase_allocations.amount_yen")?,
        ));
    }

    let payment_rows = payments::purchase_payments
        .filter(payments::purchase_id.eq(purchase_id))
        .order_by(payments::line_no.asc())
        .for_update()
        .select((
            payments::household_id,
            payments::line_no,
            payments::account_id,
            payments::amount_yen,
        ))
        .load::<(Uuid, i32, Uuid, i64)>(conn)
        .await?;
    let mut payment_rows = payment_rows
        .into_iter()
        .map(|(household_id, line_no, account_id, amount_yen)| {
            (household_id, line_no, account_id, amount_yen, false)
        })
        .collect::<Vec<_>>();
    if payment_rows.is_empty()
        && let Some(account_id) = purchase.4
    {
        payment_rows.push((household_id, 1, account_id, total.value(), true));
    }
    let mut raw_payments = Vec::with_capacity(payment_rows.len());
    let mut account_ids = BTreeSet::new();
    for (index, (row_household_id, line_no, account_id, amount_yen, is_fallback)) in
        payment_rows.into_iter().enumerate()
    {
        if row_household_id != household_id {
            return Err(PostPurchaseLoadError::HouseholdMismatch(
                "purchase_payments",
            ));
        }
        let expected_line_no = i32::try_from(index + 1)
            .map_err(|_| PostPurchaseLoadError::NumericOutOfRange("purchase_payments.line_no"))?;
        if line_no != expected_line_no {
            return Err(PostPurchaseLoadError::InvalidPaymentLineNumber);
        }
        let line_no = u32::try_from(line_no)
            .map_err(|_| PostPurchaseLoadError::NumericOutOfRange("purchase_payments.line_no"))?;
        account_ids.insert(account_id);
        raw_payments.push((
            account_id,
            PositiveYen::new(amount_yen).map_err(|_| {
                PostPurchaseLoadError::InvalidAmount("purchase_payments.amount_yen")
            })?,
            line_no,
            is_fallback,
        ));
    }

    let mut account_pools = BTreeMap::new();
    for account_id in account_ids {
        let account = accounts::accounts
            .filter(accounts::id.eq(account_id))
            .for_update()
            .select((accounts::household_id, accounts::pool_id))
            .first::<(Uuid, Option<Uuid>)>(conn)
            .await
            .optional()?
            .ok_or(PostPurchaseLoadError::MissingRow("payment account"))?;
        if account.0 != household_id {
            return Err(PostPurchaseLoadError::HouseholdMismatch("payment account"));
        }
        let pool_id = account.1.ok_or(PostPurchaseLoadError::MissingAccountPool)?;
        pool_ids.insert(pool_id);
        account_pools.insert(account_id, pool_id);
    }

    let mut pool_rules = BTreeMap::new();
    let mut referenced_members = BTreeSet::new();
    for pool_id in pool_ids {
        let pool = pools::pools
            .filter(pools::id.eq(pool_id))
            .for_update()
            .select((
                pools::household_id,
                pools::kind,
                pools::current_funding_version,
            ))
            .first::<(Uuid, String, Option<i32>)>(conn)
            .await
            .optional()?
            .ok_or(PostPurchaseLoadError::MissingRow("pool"))?;
        if pool.0 != household_id {
            return Err(PostPurchaseLoadError::HouseholdMismatch("pool"));
        }
        let funding_version = pool.2.ok_or(PostPurchaseLoadError::MissingFundingRule)?;
        let funding_version_u32 = u32::try_from(funding_version)
            .map_err(|_| PostPurchaseLoadError::NumericOutOfRange("pool funding version"))?;
        let (owner_member_id, settlement_policy, share_rows) = reference_data
            .load_funding_rule(conn, household_id, pool_id, funding_version)
            .await?
            .ok_or(PostPurchaseLoadError::MissingFundingRule)?;
        let basis = match pool.1.as_str() {
            "PERSONAL" => {
                if settlement_policy.is_some() || !share_rows.is_empty() {
                    return Err(PostPurchaseLoadError::IncompleteFundingRule(
                        "personal pool rule must have an owner and no shares or settlement policy",
                    ));
                }
                let owner = owner_member_id.ok_or(PostPurchaseLoadError::IncompleteFundingRule(
                    "personal pool rule has no owner",
                ))?;
                referenced_members.insert(owner);
                MemberFundingBasis::personal(
                    funding_version_u32,
                    MemberId::new(owner.to_string()).map_err(|_| {
                        PostPurchaseLoadError::InvalidIdentifier("funding rule owner")
                    })?,
                )
                .map_err(PostPurchaseLoadError::InvalidFundingBasis)?
            }
            "SHARED" => {
                if owner_member_id.is_some() {
                    return Err(PostPurchaseLoadError::IncompleteFundingRule(
                        "shared pool rule has an owner",
                    ));
                }
                let policy =
                    settlement_policy.ok_or(PostPurchaseLoadError::IncompleteFundingRule(
                        "shared pool rule has no settlement policy",
                    ))?;
                let policy = match policy.as_str() {
                    "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
                    "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
                    value => {
                        return Err(PostPurchaseLoadError::InvalidEnum {
                            field: "pool_funding_rules.settlement_policy",
                            value: value.to_owned(),
                        });
                    }
                };
                let mut weights = Vec::with_capacity(share_rows.len());
                for (member_id, weight) in share_rows {
                    referenced_members.insert(member_id);
                    let weight = u64::try_from(weight).map_err(|_| {
                        PostPurchaseLoadError::NumericOutOfRange("pool_funding_shares.weight")
                    })?;
                    weights.push(
                        MemberWeight::new(
                            MemberId::new(member_id.to_string()).map_err(|_| {
                                PostPurchaseLoadError::InvalidIdentifier(
                                    "pool_funding_shares.member_id",
                                )
                            })?,
                            weight,
                        )
                        .map_err(PostPurchaseLoadError::InvalidFundingBasis)?,
                    );
                }
                MemberFundingBasis::shared(funding_version_u32, weights, policy)
                    .map_err(PostPurchaseLoadError::InvalidFundingBasis)?
            }
            value => {
                return Err(PostPurchaseLoadError::InvalidEnum {
                    field: "pools.kind",
                    value: value.to_owned(),
                });
            }
        };
        pool_rules.insert(pool_id, basis);
    }

    let allocations = raw_allocations
        .into_iter()
        .map(|(item_id, pool_id, amount)| {
            let funding = pool_rules
                .get(&pool_id)
                .ok_or(PostPurchaseLoadError::MissingFundingRule)?
                .clone();
            Ok(PoolAllocation {
                item_id: ItemId::new(item_id.to_string()).map_err(|_| {
                    PostPurchaseLoadError::InvalidIdentifier("purchase_allocations.item_id")
                })?,
                pool_id: PoolId::new(pool_id.to_string()).map_err(|_| {
                    PostPurchaseLoadError::InvalidIdentifier("purchase_allocations.pool_id")
                })?,
                amount,
                funding,
            })
        })
        .collect::<Result<Vec<_>, PostPurchaseLoadError>>()?;

    let mut payment_values = Vec::with_capacity(raw_payments.len());
    let mut resolved_payments = Vec::with_capacity(raw_payments.len());
    for (account_id, amount, line_no, is_fallback) in raw_payments {
        let pool_id = *account_pools
            .get(&account_id)
            .ok_or(PostPurchaseLoadError::MissingAccountPool)?;
        let rule = pool_rules
            .get(&pool_id)
            .ok_or(PostPurchaseLoadError::MissingFundingRule)?
            .clone();
        let workflow_account_id = AccountId::new(account_id.to_string())
            .map_err(|_| PostPurchaseLoadError::InvalidIdentifier("account_id"))?;
        payment_values.push(Payment {
            account_id: workflow_account_id.clone(),
            amount,
            funding: PaymentFundingBasis {
                pool_id: PoolId::new(pool_id.to_string())
                    .map_err(|_| PostPurchaseLoadError::InvalidIdentifier("account pool_id"))?,
                rule,
            },
        });
        resolved_payments.push(ResolvedPostPurchasePayment {
            line_no,
            account_id: workflow_account_id,
            amount,
            is_fallback,
        });
    }

    // funding ruleの外部キー制約でmembershipを保証するが、commandを返す前にhouseholdとの関連を確認する。
    if !referenced_members.is_empty() {
        let found = household_members::household_members
            .filter(household_members::household_id.eq(household_id))
            .filter(household_members::member_id.eq_any(referenced_members.iter().copied()))
            .for_update()
            .select(household_members::member_id)
            .load::<Uuid>(conn)
            .await?
            .into_iter()
            .collect::<BTreeSet<_>>();
        if found != referenced_members {
            return Err(PostPurchaseLoadError::HouseholdMismatch(
                "funding rule member",
            ));
        }
    }

    Ok(LoadedPostPurchaseCommand {
        command: PostPurchaseCommand {
            draft: DraftPurchase::reconstitute(
                draft_id,
                revision,
                total,
                purchase_items,
                allocations,
                payment_values,
            )
            .map_err(PostPurchaseLoadError::InvalidDraftPurchase)?,
            expected_revision,
            period,
        },
        resolved_payments,
    })
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, PostPurchaseLoadError> {
    Uuid::parse_str(value).map_err(|_| PostPurchaseLoadError::InvalidUuid(field))
}

fn yen(value: i64, field: &'static str) -> Result<Yen, PostPurchaseLoadError> {
    Yen::new(value).map_err(|_| PostPurchaseLoadError::InvalidAmount(field))
}
