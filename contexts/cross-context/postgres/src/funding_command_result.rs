use std::collections::HashMap;

use bookkeeping_domain::SettlementPolicy;
use chrono::{DateTime, Utc};
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use policy_application::settlement_data::SettlementDataRead;
use uuid::Uuid;

use horse_postgres_common::{PgPool, schema::public};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingLedgerMember {
    pub id: Uuid,
    pub display_name: String,
    pub role: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingPoolShare {
    pub member_id: Uuid,
    pub member_name: String,
    pub weight: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingLedgerPool {
    pub id: Uuid,
    pub name: String,
    pub kind: String,
    pub default_share_weight: i64,
    pub funding_shares: Vec<FundingPoolShare>,
    pub funding_rule_version: i32,
    pub owner_member_id: Option<Uuid>,
    pub settlement_policy: Option<SettlementPolicy>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingLedgerEntryResult {
    pub id: Uuid,
    pub household_id: Uuid,
    pub occurred_at: DateTime<Utc>,
    pub description: String,
    pub amount_yen: i64,
    pub currency: String,
    pub created_at: DateTime<Utc>,
    pub member: FundingLedgerMember,
    pub pool: FundingLedgerPool,
}

#[derive(Debug)]
pub enum FundingCommandResultError {
    Pool(String),
    Database(DieselError),
    MissingFundingMember,
    MissingFundingPool,
    InvalidMemberRole(String),
    InvalidPoolKind(String),
    InvalidSettlementPolicy(String),
}

impl From<DieselError> for FundingCommandResultError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn load_funding_ledger_entry_result(
    pool: &PgPool,
    household_id: Uuid,
    ledger_entry_id: Uuid,
) -> Result<FundingLedgerEntryResult, FundingCommandResultError> {
    let mut conn = pool
        .get()
        .await
        .map_err(|error| FundingCommandResultError::Pool(error.to_string()))?;
    use public::ledger_entries::dsl as ledger;

    let (id, household_id, occurred_at, description, amount_yen, member_id, pool_id, created_at) =
        ledger::ledger_entries
            .filter(ledger::household_id.eq(household_id))
            .filter(ledger::id.eq(ledger_entry_id))
            .filter(ledger::source_kind.eq_any(["FUNDING", "funding"]))
            .select((
                ledger::id,
                ledger::household_id,
                ledger::occurred_at,
                ledger::description,
                ledger::amount_yen,
                ledger::from_member_id,
                ledger::pool_id,
                ledger::created_at,
            ))
            .first::<(
                Uuid,
                Uuid,
                DateTime<Utc>,
                String,
                i64,
                Option<Uuid>,
                Option<Uuid>,
                DateTime<Utc>,
            )>(&mut *conn)
            .await?;

    let member_id = member_id.ok_or(FundingCommandResultError::MissingFundingMember)?;
    let pool_id = pool_id.ok_or(FundingCommandResultError::MissingFundingPool)?;
    let member = load_member(&mut conn, household_id, member_id).await?;
    let pool = load_pool(pool, &mut conn, household_id, pool_id).await?;

    Ok(FundingLedgerEntryResult {
        id,
        household_id,
        occurred_at,
        description,
        amount_yen,
        currency: "JPY".to_owned(),
        created_at,
        member,
        pool,
    })
}

pub async fn load_funding_ledger_entry_results(
    pool: &PgPool,
    household_ids: &[Uuid],
    ledger_entry_ids: &[Uuid],
) -> Result<std::collections::HashMap<Uuid, FundingLedgerEntryResult>, FundingCommandResultError> {
    use std::collections::HashMap;

    let mut result = HashMap::with_capacity(ledger_entry_ids.len());
    if household_ids.is_empty() || ledger_entry_ids.is_empty() {
        return Ok(result);
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| FundingCommandResultError::Pool(error.to_string()))?;

    use public::ledger_entries::dsl as ledger;
    let ledger_rows = ledger::ledger_entries
        .filter(ledger::household_id.eq_any(household_ids))
        .filter(ledger::id.eq_any(ledger_entry_ids))
        .filter(ledger::source_kind.eq_any(["FUNDING", "funding"]))
        .select((
            ledger::id,
            ledger::household_id,
            ledger::occurred_at,
            ledger::description,
            ledger::amount_yen,
            ledger::from_member_id,
            ledger::pool_id,
            ledger::created_at,
        ))
        .load::<(
            Uuid,
            Uuid,
            DateTime<Utc>,
            String,
            i64,
            Option<Uuid>,
            Option<Uuid>,
            DateTime<Utc>,
        )>(&mut *conn)
        .await?;
    if ledger_rows.is_empty() {
        return Ok(result);
    }

    let mut member_ids = Vec::with_capacity(ledger_rows.len());
    let mut pool_ids = Vec::with_capacity(ledger_rows.len());
    for (_, _, _, _, _, member_id, pool_id, _) in &ledger_rows {
        member_ids.push(member_id.ok_or(FundingCommandResultError::MissingFundingMember)?);
        pool_ids.push(pool_id.ok_or(FundingCommandResultError::MissingFundingPool)?);
    }
    member_ids.sort_unstable();
    member_ids.dedup();
    pool_ids.sort_unstable();
    pool_ids.dedup();

    let members = public::members::table
        .filter(public::members::id.eq_any(&member_ids))
        .select((public::members::id, public::members::display_name))
        .load::<(Uuid, String)>(&mut *conn)
        .await?
        .into_iter()
        .collect::<HashMap<_, _>>();
    let roles = public::household_members::table
        .filter(public::household_members::household_id.eq_any(household_ids))
        .filter(public::household_members::member_id.eq_any(&member_ids))
        .select((
            public::household_members::household_id,
            public::household_members::member_id,
            public::household_members::role,
        ))
        .load::<(Uuid, Uuid, String)>(&mut *conn)
        .await?
        .into_iter()
        .map(|(household_id, member_id, role)| ((household_id, member_id), role))
        .collect::<HashMap<_, _>>();

    let pool_rows = public::pools::table
        .filter(public::pools::household_id.eq_any(household_ids))
        .filter(public::pools::id.eq_any(&pool_ids))
        .select((
            public::pools::id,
            public::pools::household_id,
            public::pools::name,
            public::pools::kind,
            public::pools::current_funding_version,
        ))
        .load::<(Uuid, Uuid, String, String, Option<i32>)>(&mut *conn)
        .await?;
    let pool_rows = pool_rows
        .into_iter()
        .map(|(id, household_id, name, kind, version)| ((household_id, id), (name, kind, version)))
        .collect::<HashMap<_, _>>();
    let default_shares = public::households::table
        .inner_join(
            public::allocation_defaults::table.on(public::households::id
                .eq(public::allocation_defaults::household_id)
                .and(
                    public::households::default_allocation_version
                        .eq(public::allocation_defaults::version),
                )),
        )
        .filter(public::households::id.eq_any(household_ids))
        .filter(public::allocation_defaults::pool_id.eq_any(&pool_ids))
        .select((
            public::households::id,
            public::allocation_defaults::pool_id,
            public::allocation_defaults::weight,
        ))
        .load::<(Uuid, Uuid, i64)>(&mut *conn)
        .await?
        .into_iter()
        .map(|(household_id, pool_id, weight)| ((household_id, pool_id), weight))
        .collect::<HashMap<_, _>>();

    let policy_reader =
        policy_postgres::settlement_data::PostgresSettlementDataRead::new(pool.clone());
    let mut rules = HashMap::new();
    let mut raw_shares_by_rule = HashMap::<(Uuid, Uuid, i32), Vec<(Uuid, i64)>>::new();
    let mut share_member_ids = Vec::new();
    for household_id in household_ids {
        let policy = policy_reader
            .rules_and_shares(*household_id)
            .await
            .map_err(FundingCommandResultError::Pool)?;
        for rule in policy.rules {
            rules.insert(
                (*household_id, rule.pool_id, rule.version),
                (rule.owner_member_id, rule.settlement_policy),
            );
        }
        for share in policy.shares {
            share_member_ids.push(share.member_id);
            raw_shares_by_rule
                .entry((*household_id, share.pool_id, share.version))
                .or_default()
                .push((share.member_id, share.weight));
        }
    }
    share_member_ids.sort_unstable();
    share_member_ids.dedup();
    let share_member_names = if share_member_ids.is_empty() {
        HashMap::new()
    } else {
        public::members::table
            .filter(public::members::id.eq_any(&share_member_ids))
            .select((public::members::id, public::members::display_name))
            .load::<(Uuid, String)>(&mut *conn)
            .await?
            .into_iter()
            .collect::<HashMap<_, _>>()
    };
    let mut shares_by_rule = HashMap::<(Uuid, Uuid, i32), Vec<FundingPoolShare>>::new();
    for (key, shares) in raw_shares_by_rule {
        let values = shares_by_rule.entry(key).or_default();
        for (member_id, weight) in shares {
            let member_name = share_member_names
                .get(&member_id)
                .cloned()
                .ok_or(FundingCommandResultError::MissingFundingMember)?;
            values.push(FundingPoolShare {
                member_id,
                member_name,
                weight,
            });
        }
    }

    for (id, household_id, occurred_at, description, amount_yen, member_id, pool_id, created_at) in
        ledger_rows
    {
        let member_id = member_id.ok_or(FundingCommandResultError::MissingFundingMember)?;
        let pool_id = pool_id.ok_or(FundingCommandResultError::MissingFundingPool)?;
        let display_name = members
            .get(&member_id)
            .cloned()
            .ok_or(FundingCommandResultError::MissingFundingMember)?;
        let role = roles
            .get(&(household_id, member_id))
            .cloned()
            .unwrap_or_else(|| "MEMBER".to_owned());
        if !matches!(role.as_str(), "OWNER" | "MEMBER") {
            return Err(FundingCommandResultError::InvalidMemberRole(role));
        }
        let member = FundingLedgerMember {
            id: member_id,
            display_name,
            role,
        };
        let (name, kind, current_version) = pool_rows
            .get(&(household_id, pool_id))
            .cloned()
            .ok_or(FundingCommandResultError::MissingFundingPool)?;
        if !matches!(kind.as_str(), "PERSONAL" | "SHARED") {
            return Err(FundingCommandResultError::InvalidPoolKind(kind));
        }
        let default_share_weight = default_shares
            .get(&(household_id, pool_id))
            .copied()
            .unwrap_or_default();
        let mut owner_member_id = None;
        let mut settlement_policy = None;
        let mut funding_shares = Vec::new();
        if let Some(version) = current_version
            && let Some((owner, policy)) = rules.get(&(household_id, pool_id, version))
        {
            owner_member_id = *owner;
            if let Some(policy) = policy.as_deref() {
                settlement_policy = Some(match policy {
                    "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
                    "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
                    value => {
                        return Err(FundingCommandResultError::InvalidSettlementPolicy(
                            value.to_owned(),
                        ));
                    }
                });
            }
            funding_shares = shares_by_rule
                .get(&(household_id, pool_id, version))
                .cloned()
                .unwrap_or_default();
        }
        result.insert(
            id,
            FundingLedgerEntryResult {
                id,
                household_id,
                occurred_at,
                description,
                amount_yen,
                currency: "JPY".to_owned(),
                created_at,
                member,
                pool: FundingLedgerPool {
                    id: pool_id,
                    name,
                    kind,
                    default_share_weight,
                    funding_shares,
                    funding_rule_version: current_version.unwrap_or_default(),
                    owner_member_id,
                    settlement_policy,
                },
            },
        );
    }
    Ok(result)
}

async fn load_member(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    member_id: Uuid,
) -> Result<FundingLedgerMember, FundingCommandResultError> {
    let member = public::household_members::table
        .inner_join(
            public::members::table.on(public::household_members::member_id.eq(public::members::id)),
        )
        .filter(public::household_members::household_id.eq(household_id))
        .filter(public::household_members::member_id.eq(member_id))
        .select((
            public::members::id,
            public::members::display_name,
            public::household_members::role,
        ))
        .first::<(Uuid, String, String)>(conn)
        .await
        .optional()?;
    let member = if let Some(member) = member {
        member
    } else {
        let (id, display_name) = public::members::table
            .filter(public::members::id.eq(member_id))
            .select((public::members::id, public::members::display_name))
            .first::<(Uuid, String)>(conn)
            .await
            .optional()?
            .ok_or(FundingCommandResultError::MissingFundingMember)?;
        (id, display_name, "MEMBER".to_owned())
    };
    if !matches!(member.2.as_str(), "OWNER" | "MEMBER") {
        return Err(FundingCommandResultError::InvalidMemberRole(member.2));
    }
    Ok(FundingLedgerMember {
        id: member.0,
        display_name: member.1,
        role: member.2,
    })
}

async fn load_pool(
    pool: &PgPool,
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    pool_id: Uuid,
) -> Result<FundingLedgerPool, FundingCommandResultError> {
    let (id, name, kind, funding_rule_version) = public::pools::table
        .filter(public::pools::household_id.eq(household_id))
        .filter(public::pools::id.eq(pool_id))
        .select((
            public::pools::id,
            public::pools::name,
            public::pools::kind,
            public::pools::current_funding_version,
        ))
        .first::<(Uuid, String, String, Option<i32>)>(conn)
        .await
        .optional()?
        .ok_or(FundingCommandResultError::MissingFundingPool)?;
    if !matches!(kind.as_str(), "PERSONAL" | "SHARED") {
        return Err(FundingCommandResultError::InvalidPoolKind(kind));
    }

    let default_share_weight = public::households::table
        .inner_join(
            public::allocation_defaults::table
                .on(public::households::id.eq(public::allocation_defaults::household_id)),
        )
        .filter(public::households::id.eq(household_id))
        .filter(
            public::allocation_defaults::version.eq(public::households::default_allocation_version),
        )
        .filter(public::allocation_defaults::pool_id.eq(pool_id))
        .select(public::allocation_defaults::weight)
        .first::<i64>(conn)
        .await
        .optional()?
        .unwrap_or_default();

    let mut owner_member_id = None;
    let mut settlement_policy = None;
    let mut funding_shares = Vec::new();
    if let Some(version) = funding_rule_version {
        let policy =
            policy_postgres::settlement_data::PostgresSettlementDataRead::new(pool.clone())
                .rules_and_shares(household_id)
                .await
                .map_err(FundingCommandResultError::Pool)?;
        if let Some(rule) = policy
            .rules
            .into_iter()
            .find(|rule| rule.pool_id == pool_id && rule.version == version)
        {
            owner_member_id = rule.owner_member_id;
            if let Some(policy) = rule.settlement_policy {
                settlement_policy = Some(match policy.as_str() {
                    "BALANCE_TO_STANDARD" => SettlementPolicy::BalanceToStandard,
                    "REIMBURSE_PERSONAL" => SettlementPolicy::ReimbursePersonal,
                    _ => return Err(FundingCommandResultError::InvalidSettlementPolicy(policy)),
                });
            }
            let shares = policy
                .shares
                .into_iter()
                .filter(|share| share.pool_id == pool_id && share.version == version)
                .collect::<Vec<_>>();
            let member_ids = shares
                .iter()
                .map(|share| share.member_id)
                .collect::<Vec<_>>();
            let member_names = if member_ids.is_empty() {
                HashMap::new()
            } else {
                public::members::table
                    .filter(public::members::id.eq_any(&member_ids))
                    .select((public::members::id, public::members::display_name))
                    .load::<(Uuid, String)>(conn)
                    .await?
                    .into_iter()
                    .collect::<HashMap<_, _>>()
            };
            funding_shares = shares
                .into_iter()
                .map(|share| {
                    let member_name = member_names
                        .get(&share.member_id)
                        .cloned()
                        .ok_or(FundingCommandResultError::MissingFundingMember)?;
                    Ok(FundingPoolShare {
                        member_id: share.member_id,
                        member_name,
                        weight: share.weight,
                    })
                })
                .collect::<Result<Vec<_>, FundingCommandResultError>>()?;
        }
    }

    Ok(FundingLedgerPool {
        id,
        name,
        kind,
        default_share_weight,
        funding_shares,
        funding_rule_version: funding_rule_version.unwrap_or_default(),
        owner_member_id,
        settlement_policy,
    })
}
