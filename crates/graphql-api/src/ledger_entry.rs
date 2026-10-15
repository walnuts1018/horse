use async_graphql::{ID, Interface, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::funding_command_result::FundingLedgerEntryResult;
use uuid::Uuid;

#[derive(Clone, Debug, SimpleObject)]
pub struct FundingLedgerEntry {
    id: ID,
    occurred_at: crate::DateTime,
    description: String,
    amount_yen: crate::Long,
    currency: String,
    created_at: crate::DateTime,
    member: crate::member_pool::Member,
    pool: crate::member_pool::Pool,
}

#[derive(Clone, Debug, Interface)]
#[graphql(field(name = "id", ty = "&ID"))]
pub enum Node {
    FundingLedgerEntry(FundingLedgerEntry),
}

#[derive(Clone, Debug, Interface)]
#[graphql(
    field(name = "id", ty = "&ID"),
    field(name = "occurred_at", ty = "&crate::DateTime"),
    field(name = "description", ty = "&String"),
    field(name = "amount_yen", ty = "&crate::Long"),
    field(name = "currency", ty = "&String"),
    field(name = "created_at", ty = "&crate::DateTime")
)]
pub enum LedgerEntry {
    FundingLedgerEntry(FundingLedgerEntry),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FundingLedgerEntryMappingError {
    InvalidMemberRole,
    InvalidPoolKind,
    InvalidSettlementPolicy,
}

impl TryFrom<FundingLedgerEntryResult> for FundingLedgerEntry {
    type Error = FundingLedgerEntryMappingError;

    fn try_from(value: FundingLedgerEntryResult) -> Result<Self, Self::Error> {
        use crate::member_pool::{
            MemberProjection, MemberRole, PoolFundingShareProjection, PoolKind, PoolProjection,
            SharedPoolSettlementPolicy,
        };

        let role = match value.member.role.as_str() {
            "OWNER" => MemberRole::Owner,
            "MEMBER" => MemberRole::Member,
            _ => return Err(FundingLedgerEntryMappingError::InvalidMemberRole),
        };
        let kind = match value.pool.kind.as_str() {
            "PERSONAL" => PoolKind::Personal,
            "SHARED" => PoolKind::Shared,
            _ => return Err(FundingLedgerEntryMappingError::InvalidPoolKind),
        };
        let settlement_policy = value
            .pool
            .settlement_policy
            .as_deref()
            .map(|policy| match policy {
                "BALANCE_TO_STANDARD" => Ok(SharedPoolSettlementPolicy::BalanceToStandard),
                "REIMBURSE_PERSONAL" => Ok(SharedPoolSettlementPolicy::ReimbursePersonal),
                _ => Err(FundingLedgerEntryMappingError::InvalidSettlementPolicy),
            })
            .transpose()?;

        let member = MemberProjection {
            id: value.member.id,
            display_name: value.member.display_name,
            role,
        }
        .into();
        let pool = PoolProjection {
            id: value.pool.id,
            kind,
            name: value.pool.name,
            default_share_weight: value.pool.default_share_weight,
            funding_shares: value
                .pool
                .funding_shares
                .into_iter()
                .map(|share| PoolFundingShareProjection {
                    member_id: share.member_id,
                    member_name: share.member_name,
                    weight: share.weight,
                })
                .collect(),
            funding_rule_version: value.pool.funding_rule_version,
            owner_member_id: value.pool.owner_member_id,
            settlement_policy,
        }
        .into();

        Ok(Self {
            id: encode_node_id("LedgerEntry", value.id),
            occurred_at: value.occurred_at.into(),
            description: value.description,
            amount_yen: crate::Long(value.amount_yen),
            currency: value.currency,
            created_at: value.created_at.into(),
            member,
            pool,
        })
    }
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}
