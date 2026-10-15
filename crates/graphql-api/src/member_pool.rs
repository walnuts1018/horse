use async_graphql::{Enum, ID, Interface, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use uuid::Uuid;

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum MemberRole {
    Owner,
    Member,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct Member {
    id: ID,
    display_name: String,
    role: MemberRole,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum PoolKind {
    Personal,
    Shared,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum SharedPoolSettlementPolicy {
    BalanceToStandard,
    ReimbursePersonal,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PoolFundingShare {
    member_id: ID,
    member_name: String,
    weight: crate::Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PersonalPool {
    id: ID,
    name: String,
    default_share_weight: crate::Long,
    funding_shares: Vec<PoolFundingShare>,
    funding_rule_version: i32,
    owner_member_id: ID,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UnconfiguredPersonalPool {
    id: ID,
    name: String,
    default_share_weight: crate::Long,
    funding_shares: Vec<PoolFundingShare>,
    funding_rule_version: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct SharedPool {
    id: ID,
    name: String,
    default_share_weight: crate::Long,
    funding_shares: Vec<PoolFundingShare>,
    funding_rule_version: i32,
    settlement_policy: SharedPoolSettlementPolicy,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct UnconfiguredSharedPool {
    id: ID,
    name: String,
    default_share_weight: crate::Long,
    funding_shares: Vec<PoolFundingShare>,
    funding_rule_version: i32,
}

#[derive(Clone, Debug, Interface)]
#[graphql(
    field(name = "id", ty = "&ID"),
    field(name = "name", ty = "&String"),
    field(name = "default_share_weight", ty = "&crate::Long"),
    field(name = "funding_shares", ty = "&Vec<PoolFundingShare>"),
    field(name = "funding_rule_version", ty = "&i32")
)]
pub enum Pool {
    PersonalPool(PersonalPool),
    UnconfiguredPersonalPool(UnconfiguredPersonalPool),
    SharedPool(SharedPool),
    UnconfiguredSharedPool(UnconfiguredSharedPool),
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MemberProjection {
    pub id: Uuid,
    pub display_name: String,
    pub role: MemberRole,
}

impl From<MemberProjection> for Member {
    fn from(value: MemberProjection) -> Self {
        Self {
            id: encode_node_id("Member", value.id),
            display_name: value.display_name,
            role: value.role,
        }
    }
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PoolFundingShareProjection {
    pub member_id: Uuid,
    pub member_name: String,
    pub weight: i64,
}

#[derive(Clone, Debug, Eq, PartialEq)]
pub struct PoolProjection {
    pub id: Uuid,
    pub kind: PoolKind,
    pub name: String,
    pub default_share_weight: i64,
    pub funding_shares: Vec<PoolFundingShareProjection>,
    pub funding_rule_version: i32,
    pub owner_member_id: Option<Uuid>,
    pub settlement_policy: Option<SharedPoolSettlementPolicy>,
}

impl From<PoolFundingShareProjection> for PoolFundingShare {
    fn from(value: PoolFundingShareProjection) -> Self {
        Self {
            member_id: encode_node_id("Member", value.member_id),
            member_name: value.member_name,
            weight: crate::Long(value.weight),
        }
    }
}

impl From<PoolProjection> for Pool {
    fn from(value: PoolProjection) -> Self {
        let id = encode_node_id("Pool", value.id);
        let funding_shares = value.funding_shares.into_iter().map(Into::into).collect();
        let default_share_weight = crate::Long(value.default_share_weight);
        match value.kind {
            PoolKind::Personal => match value.owner_member_id {
                Some(owner_member_id) => Self::PersonalPool(PersonalPool {
                    id,
                    name: value.name,
                    default_share_weight,
                    funding_shares,
                    funding_rule_version: value.funding_rule_version,
                    owner_member_id: encode_node_id("Member", owner_member_id),
                }),
                None => Self::UnconfiguredPersonalPool(UnconfiguredPersonalPool {
                    id,
                    name: value.name,
                    default_share_weight,
                    funding_shares,
                    funding_rule_version: value.funding_rule_version,
                }),
            },
            PoolKind::Shared => match (value.settlement_policy, funding_shares.is_empty()) {
                (Some(settlement_policy), false) => Self::SharedPool(SharedPool {
                    id,
                    name: value.name,
                    default_share_weight,
                    funding_shares,
                    funding_rule_version: value.funding_rule_version,
                    settlement_policy,
                }),
                _ => Self::UnconfiguredSharedPool(UnconfiguredSharedPool {
                    id,
                    name: value.name,
                    default_share_weight,
                    funding_shares,
                    funding_rule_version: value.funding_rule_version,
                }),
            },
        }
    }
}

fn encode_node_id(kind: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}
