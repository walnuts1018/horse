use async_graphql::{Context, Error, ID, InputObject, Interface, Result, SimpleObject, Union};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::Utc;
use horse_postgres_common::PgPool;
use household_application::{
    create_household::CreateHouseholdCommand,
    invitation::{CreateInvitationCommand, RevokeInvitationCommand},
    ports::{CreateHouseholdPortError, HouseholdStore},
};
use household_postgres::{
    create_household as postgres_household, household_invitation as postgres_invitation,
};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::MutationErrorCode;

#[derive(Clone, Debug, InputObject)]
pub struct CreateHouseholdInput {
    name: String,
    member_names: Vec<String>,
}

#[derive(Clone, Debug, InputObject)]
pub struct CreateHouseholdInvitationInput {
    household_id: ID,
    expires_at: crate::DateTime,
}

#[derive(Clone, Debug, InputObject)]
pub struct AcceptHouseholdInvitationInput {
    token: String,
}

#[derive(Clone, Debug, InputObject)]
pub struct RevokeHouseholdInvitationInput {
    household_id: ID,
    invitation_id: ID,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct Household {
    pub(crate) id: ID,
    name: String,
    revision: i32,
    members: Vec<crate::member_pool::Member>,
    pools: Vec<crate::member_pool::Pool>,
    accounts: Vec<crate::account_mutations::AccountObject>,
    categories: Vec<crate::category::Category>,
    default_allocation: Vec<HouseholdAllocation>,
    created_at: crate::DateTime,
    updated_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdAllocation {
    pool_id: ID,
    pool_name: String,
    weight: crate::Long,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct PendingHouseholdInvitation {
    pub(crate) id: ID,
    household_id: ID,
    expires_at: crate::DateTime,
    created_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AcceptedHouseholdInvitation {
    pub(crate) id: ID,
    household_id: ID,
    expires_at: crate::DateTime,
    created_at: crate::DateTime,
    accepted_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RevokedHouseholdInvitation {
    pub(crate) id: ID,
    household_id: ID,
    expires_at: crate::DateTime,
    created_at: crate::DateTime,
    revoked_at: crate::DateTime,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct ExpiredHouseholdInvitation {
    pub(crate) id: ID,
    household_id: ID,
    expires_at: crate::DateTime,
    created_at: crate::DateTime,
}

#[allow(clippy::duplicated_attributes)]
#[derive(Clone, Debug, Interface)]
#[graphql(
    field(name = "id", ty = "&ID"),
    field(name = "household_id", ty = "&ID"),
    field(name = "expires_at", ty = "&crate::DateTime"),
    field(name = "created_at", ty = "&crate::DateTime")
)]
pub enum HouseholdInvitation {
    PendingHouseholdInvitation(PendingHouseholdInvitation),
    AcceptedHouseholdInvitation(AcceptedHouseholdInvitation),
    RevokedHouseholdInvitation(RevokedHouseholdInvitation),
    ExpiredHouseholdInvitation(ExpiredHouseholdInvitation),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreatedHouseholdInvitation {
    invitation: HouseholdInvitation,
    token: String,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdInvitationEdge {
    cursor: String,
    node: HouseholdInvitation,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdInvitationConnection {
    edges: Vec<HouseholdInvitationEdge>,
    page_info: crate::pagination::PageInfo,
    total_count: i32,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateHouseholdSuccess {
    result: Household,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct AcceptHouseholdInvitationSuccess {
    result: Household,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct CreateHouseholdInvitationSuccess {
    result: CreatedHouseholdInvitation,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RevokeHouseholdInvitationSuccess {
    result: HouseholdInvitation,
}

macro_rules! user_error {
    ($type:ident) => {
        #[derive(Clone, Debug, SimpleObject)]
        pub struct $type {
            pub code: MutationErrorCode,
            pub message: String,
        }

        impl $type {
            fn new(code: MutationErrorCode) -> Self {
                Self {
                    code,
                    message: error_message(code).to_owned(),
                }
            }
        }
    };
}

user_error!(CreateHouseholdUserError);
user_error!(CreateHouseholdInvitationUserError);
user_error!(AcceptHouseholdInvitationUserError);
user_error!(RevokeHouseholdInvitationUserError);

#[derive(Union)]
pub enum CreateHouseholdResult {
    Success(CreateHouseholdSuccess),
    UserError(CreateHouseholdUserError),
}

#[derive(Union)]
pub enum CreateHouseholdInvitationResult {
    Success(CreateHouseholdInvitationSuccess),
    UserError(CreateHouseholdInvitationUserError),
}

#[derive(Union)]
pub enum AcceptHouseholdInvitationResult {
    Success(AcceptHouseholdInvitationSuccess),
    UserError(AcceptHouseholdInvitationUserError),
}

#[derive(Union)]
pub enum RevokeHouseholdInvitationResult {
    Success(RevokeHouseholdInvitationSuccess),
    UserError(RevokeHouseholdInvitationUserError),
}

pub async fn create_household(
    ctx: &Context<'_>,
    input: CreateHouseholdInput,
) -> Result<CreateHouseholdResult> {
    let actor = crate::actor(ctx)?;
    // §4の依存方向に従い、postgresの具体関数ではなくApplication層のport契約経由で操作する。
    let store = postgres_household::PostgresHouseholds::new(ctx.data::<PgPool>()?.clone());
    match store
        .create_household(CreateHouseholdCommand {
            actor_id: actor.0.to_string(),
            name: input.name,
            member_names: input.member_names,
        })
        .await
    {
        Ok(created) => {
            let id = parse_uuid(&created.household_id)?;
            match postgres_household::load_household(ctx.data::<PgPool>()?, actor.0, id).await {
                Ok(household) => Ok(CreateHouseholdSuccess {
                    result: map_household(household)?,
                }
                .into()),
                Err(error) => Err(crate::internal_error(error)),
            }
        }
        Err(error) => match create_household_error_code(&error) {
            Some(code) => Ok(CreateHouseholdUserError::new(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn create_household_invitation(
    ctx: &Context<'_>,
    input: CreateHouseholdInvitationInput,
) -> Result<CreateHouseholdInvitationResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => {
            return Ok(CreateHouseholdInvitationUserError::new(MutationErrorCode::BadInput).into());
        }
    };
    let expires_at = match input.expires_at.parse_utc() {
        Ok(value) => value,
        Err(_) => {
            return Ok(CreateHouseholdInvitationUserError::new(MutationErrorCode::BadInput).into());
        }
    };
    let mut token_bytes = Vec::with_capacity(32);
    token_bytes.extend_from_slice(Uuid::new_v4().as_bytes());
    token_bytes.extend_from_slice(Uuid::new_v4().as_bytes());
    let token = URL_SAFE_NO_PAD.encode(&token_bytes);
    let token_hash: [u8; 32] = Sha256::digest(&token_bytes).into();
    let now = Utc::now();
    let command = CreateInvitationCommand {
        actor_id: actor.0.to_string(),
        household_id: household_id.to_string(),
        invitation_id: Uuid::now_v7().to_string(),
        expires_at,
    };
    match postgres_invitation::create_invitation(
        ctx.data::<PgPool>()?,
        command,
        token,
        token_hash,
        now,
    )
    .await
    {
        Ok(created) => Ok(CreateHouseholdInvitationSuccess {
            result: CreatedHouseholdInvitation {
                invitation: PendingHouseholdInvitation {
                    id: encode_node_id("HouseholdInvitation", &created.id)?,
                    household_id: encode_node_id("Household", &household_id.to_string())?,
                    expires_at: created.expires_at.into(),
                    created_at: created.created_at.into(),
                }
                .into(),
                token: created.token,
            },
        }
        .into()),
        Err(error) => match invitation_error_code(&error) {
            Some(code) => Ok(CreateHouseholdInvitationUserError::new(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn accept_household_invitation(
    ctx: &Context<'_>,
    input: AcceptHouseholdInvitationInput,
) -> Result<AcceptHouseholdInvitationResult> {
    let actor = crate::actor(ctx)?;
    let token_bytes = match URL_SAFE_NO_PAD.decode(input.token.as_bytes()) {
        Ok(bytes) if bytes.len() == 32 => bytes,
        _ => {
            return Ok(AcceptHouseholdInvitationUserError::new(MutationErrorCode::BadInput).into());
        }
    };
    let token_hash: [u8; 32] = Sha256::digest(token_bytes).into();
    match postgres_invitation::accept_invitation_by_token(
        ctx.data::<PgPool>()?,
        actor.0.to_string(),
        token_hash,
        Utc::now(),
    )
    .await
    {
        Ok(household_id) => {
            let household_id = parse_uuid(&household_id)?;
            match postgres_household::load_household(ctx.data::<PgPool>()?, actor.0, household_id)
                .await
            {
                Ok(household) => Ok(AcceptHouseholdInvitationSuccess {
                    result: map_household(household)?,
                }
                .into()),
                Err(error) => Err(crate::internal_error(error)),
            }
        }
        Err(error) => match invitation_error_code(&error) {
            Some(code) => Ok(AcceptHouseholdInvitationUserError::new(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn revoke_household_invitation(
    ctx: &Context<'_>,
    input: RevokeHouseholdInvitationInput,
) -> Result<RevokeHouseholdInvitationResult> {
    let actor = crate::actor(ctx)?;
    let household_id = match crate::decode_household_id(&input.household_id) {
        Ok(id) => id,
        Err(_) => {
            return Ok(RevokeHouseholdInvitationUserError::new(MutationErrorCode::BadInput).into());
        }
    };
    let invitation_id = match decode_node_id(&input.invitation_id, "HouseholdInvitation") {
        Ok(id) => id,
        Err(()) => {
            return Ok(RevokeHouseholdInvitationUserError::new(MutationErrorCode::BadInput).into());
        }
    };
    match postgres_invitation::revoke_invitation(
        ctx.data::<PgPool>()?,
        RevokeInvitationCommand {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
            invitation_id: invitation_id.to_string(),
        },
        Utc::now(),
    )
    .await
    {
        Ok(invitation) => Ok(RevokeHouseholdInvitationSuccess {
            result: map_invitation(invitation, Utc::now())?,
        }
        .into()),
        Err(error) => match invitation_error_code(&error) {
            Some(code) => Ok(RevokeHouseholdInvitationUserError::new(code).into()),
            None => Err(crate::internal_error(error)),
        },
    }
}

pub async fn household_invitations(
    ctx: &Context<'_>,
    household_id: ID,
    after: Option<String>,
    before: Option<String>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<HouseholdInvitationConnection> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_id)?;
    let after_id = after.as_deref().map(decode_cursor).transpose()?;
    let before_id = before.as_deref().map(decode_cursor).transpose()?;
    let page = match postgres_invitation::list_invitations(
        ctx.data::<PgPool>()?,
        actor.0,
        household_id,
        after_id,
        before_id,
        first,
        last,
    )
    .await
    {
        Ok(page) => page,
        Err(postgres_invitation::HouseholdInvitationError::HouseholdNotFound)
        | Err(postgres_invitation::HouseholdInvitationError::MemberNotFound) => {
            return Err(Error::new("not found"));
        }
        Err(postgres_invitation::HouseholdInvitationError::InvalidPagination) => {
            return Err(Error::new("invalid pagination"));
        }
        Err(postgres_invitation::HouseholdInvitationError::InvalidCursor) => {
            return Err(Error::new("invalid cursor"));
        }
        Err(error) => return Err(crate::internal_error(error)),
    };
    let rows = page
        .rows
        .into_iter()
        .map(|row| map_invitation(row, Utc::now()))
        .collect::<Result<Vec<_>>>()?;
    let mut edges = Vec::with_capacity(rows.len());
    for node in rows {
        let id = invitation_raw_id(&node)?;
        edges.push(HouseholdInvitationEdge {
            cursor: encode_cursor(&id),
            node,
        });
    }
    let start_cursor = edges.first().map(|edge| edge.cursor.clone());
    let end_cursor = edges.last().map(|edge| edge.cursor.clone());
    let total_count = i32::try_from(page.total_count)
        .map_err(|_| crate::internal_error("household invitation count exceeds GraphQL Int"))?;
    Ok(HouseholdInvitationConnection {
        edges,
        page_info: crate::pagination::PageInfo {
            has_next_page: page.has_next_page,
            has_previous_page: page.has_previous_page,
            start_cursor,
            end_cursor,
        },
        total_count,
    })
}

pub(crate) fn map_household(value: postgres_household::HouseholdReadModel) -> Result<Household> {
    use crate::member_pool::{
        MemberProjection, MemberRole, PoolFundingShareProjection, PoolKind, PoolProjection,
        SharedPoolSettlementPolicy,
    };

    let revision = i32::try_from(value.revision)
        .map_err(|_| crate::internal_error("household revision exceeds GraphQL Int"))?;
    let members = value
        .members
        .into_iter()
        .map(|member| {
            let role = match member.role.as_str() {
                "OWNER" => Ok(MemberRole::Owner),
                "MEMBER" => Ok(MemberRole::Member),
                _ => Err(crate::internal_error(
                    "invalid persisted household member role",
                )),
            }?;
            Ok(MemberProjection {
                id: member.id,
                display_name: member.display_name,
                role,
            }
            .into())
        })
        .collect::<Result<Vec<_>>>()?;
    let pools = value
        .pools
        .into_iter()
        .map(|pool| {
            let kind = match pool.kind.as_str() {
                "PERSONAL" => Ok(PoolKind::Personal),
                "SHARED" => Ok(PoolKind::Shared),
                _ => Err(crate::internal_error("invalid persisted pool kind")),
            }?;
            let settlement_policy = pool
                .settlement_policy
                .as_deref()
                .map(|value| match value {
                    "BALANCE_TO_STANDARD" => Ok(SharedPoolSettlementPolicy::BalanceToStandard),
                    "REIMBURSE_PERSONAL" => Ok(SharedPoolSettlementPolicy::ReimbursePersonal),
                    _ => Err(crate::internal_error(
                        "invalid persisted pool settlement policy",
                    )),
                })
                .transpose()?;
            Ok(PoolProjection {
                id: pool.id,
                kind,
                name: pool.name,
                default_share_weight: pool.default_share_weight,
                funding_shares: pool
                    .funding_shares
                    .into_iter()
                    .map(|share| PoolFundingShareProjection {
                        member_id: share.member_id,
                        member_name: share.member_name,
                        weight: share.weight,
                    })
                    .collect(),
                funding_rule_version: pool.funding_rule_version,
                owner_member_id: pool.owner_member_id,
                settlement_policy,
            }
            .into())
        })
        .collect::<Result<Vec<_>>>()?;
    let accounts = value
        .accounts
        .into_iter()
        .map(|account| {
            let kind = match account.kind.as_str() {
                "BANK" => Ok(crate::account_mutations::AccountKind::Bank),
                "CASH" => Ok(crate::account_mutations::AccountKind::Cash),
                "CARD" => Ok(crate::account_mutations::AccountKind::Card),
                "OTHER" => Ok(crate::account_mutations::AccountKind::Other),
                _ => Err(crate::internal_error("invalid persisted account kind")),
            }?;
            Ok(crate::account_mutations::AccountObject {
                id: encode_node_id("Account", &account.id.to_string())?,
                name: account.name,
                kind,
                card_last4: account.card_last4,
                pool_id: account
                    .pool_id
                    .map(|id| encode_node_id("Pool", &id.to_string()))
                    .transpose()?,
            })
        })
        .collect::<Result<Vec<_>>>()?;
    let categories = value
        .categories
        .into_iter()
        .map(|category| {
            Ok(crate::category::Category {
                id: encode_node_id("Category", &category.id.to_string())?,
                name: category.name,
                parent_id: category
                    .parent_id
                    .map(|id| encode_node_id("Category", &id.to_string()))
                    .transpose()?,
                archived: category.archived,
            })
        })
        .collect::<Result<Vec<_>>>()?;
    let default_allocation = value
        .default_allocation
        .into_iter()
        .map(|allocation| {
            Ok(HouseholdAllocation {
                pool_id: encode_node_id("Pool", &allocation.pool_id.to_string())?,
                pool_name: allocation.pool_name,
                weight: crate::Long(allocation.weight),
            })
        })
        .collect::<Result<Vec<_>>>()?;

    Ok(Household {
        id: encode_node_id("Household", &value.id.to_string())?,
        name: value.name,
        revision,
        members,
        pools,
        accounts,
        categories,
        default_allocation,
        created_at: value.created_at.into(),
        updated_at: value.updated_at.into(),
    })
}

pub(crate) async fn load_household_graphql(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<Household> {
    postgres_household::load_household(pool, actor_id, household_id)
        .await
        .map_err(|error| {
            load_household_error_code(&error)
                .map(|code| async_graphql::Error::new(error_message(code)))
                .unwrap_or_else(|| crate::internal_error(error))
        })
        .and_then(map_household)
}

pub(crate) async fn resolve_household_node(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    type_name: &str,
    id: Uuid,
) -> Result<Option<crate::ledger_entry::Node>> {
    let household = load_household_graphql(pool, actor_id, household_id).await?;
    Ok(resolve_household_node_from_model(&household, type_name, id))
}

pub(crate) fn resolve_household_node_from_model(
    household: &Household,
    type_name: &str,
    id: Uuid,
) -> Option<crate::ledger_entry::Node> {
    let global_id = crate::node_query::encode_global_node_id(type_name, id);
    match type_name {
        "Account" => household
            .accounts
            .iter()
            .find(|value| value.id == global_id)
            .cloned()
            .map(crate::ledger_entry::Node::Account),
        "Category" => household
            .categories
            .iter()
            .find(|value| value.id == global_id)
            .cloned()
            .map(crate::ledger_entry::Node::Category),
        "Member" => household
            .members
            .iter()
            .find(|value| value.id == global_id)
            .cloned()
            .map(crate::ledger_entry::Node::Member),
        "Pool" => household
            .pools
            .iter()
            .find(|value| value.node_id() == &global_id)
            .cloned()
            .map(crate::ledger_entry::Node::Pool),
        _ => None,
    }
}

pub(crate) fn map_invitation(
    value: postgres_invitation::InvitationStateDto,
    now: chrono::DateTime<Utc>,
) -> Result<HouseholdInvitation> {
    let expired = value.expires_at <= now;
    let id = encode_node_id("HouseholdInvitation", &value.id)?;
    let household_id = encode_node_id("Household", &value.household_id)?;
    let expires_at: crate::DateTime = value.expires_at.into();
    let created_at: crate::DateTime = value.created_at.into();
    if let Some(accepted_at) = value.accepted_at {
        Ok(AcceptedHouseholdInvitation {
            id,
            household_id,
            expires_at,
            created_at,
            accepted_at: accepted_at.into(),
        }
        .into())
    } else if let Some(revoked_at) = value.revoked_at {
        Ok(RevokedHouseholdInvitation {
            id,
            household_id,
            expires_at,
            created_at,
            revoked_at: revoked_at.into(),
        }
        .into())
    } else if expired {
        Ok(ExpiredHouseholdInvitation {
            id,
            household_id,
            expires_at,
            created_at,
        }
        .into())
    } else {
        Ok(PendingHouseholdInvitation {
            id,
            household_id,
            expires_at,
            created_at,
        }
        .into())
    }
}

fn invitation_raw_id(value: &HouseholdInvitation) -> Result<String> {
    let id = match value {
        HouseholdInvitation::PendingHouseholdInvitation(value) => &value.id,
        HouseholdInvitation::AcceptedHouseholdInvitation(value) => &value.id,
        HouseholdInvitation::RevokedHouseholdInvitation(value) => &value.id,
        HouseholdInvitation::ExpiredHouseholdInvitation(value) => &value.id,
    };
    decode_node_id(id, "HouseholdInvitation")
        .map(|id| id.to_string())
        .map_err(|_| crate::internal_error("invalid mapped invitation ID"))
}

fn decode_node_id(value: &ID, expected_kind: &str) -> std::result::Result<Uuid, ()> {
    let bytes = URL_SAFE_NO_PAD.decode(value.as_str()).map_err(|_| ())?;
    let separator = bytes.iter().position(|byte| *byte == b':').ok_or(())?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != expected_kind.as_bytes() || payload.len() != 16 {
        return Err(());
    }
    let id = Uuid::from_slice(payload).map_err(|_| ())?;
    if id.is_nil() {
        return Err(());
    }
    Ok(id)
}

fn encode_node_id(kind: &str, raw_id: &str) -> Result<ID> {
    let id = Uuid::parse_str(raw_id).map_err(crate::internal_error)?;
    if id.is_nil() {
        return Err(crate::internal_error("persisted node ID is nil"));
    }
    let mut payload = Vec::with_capacity(kind.len() + 17);
    payload.extend_from_slice(kind.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    Ok(ID(URL_SAFE_NO_PAD.encode(payload)))
}

fn encode_cursor(raw_id: &str) -> String {
    URL_SAFE_NO_PAD.encode(format!("HouseholdInvitation:{raw_id}"))
}

fn decode_cursor(cursor: &str) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(cursor)
        .map_err(|_| Error::new("invalid cursor"))?;
    let value = std::str::from_utf8(&bytes).map_err(|_| Error::new("invalid cursor"))?;
    let id = value
        .strip_prefix("HouseholdInvitation:")
        .ok_or_else(|| Error::new("invalid cursor"))?;
    Uuid::parse_str(id).map_err(|_| Error::new("invalid cursor"))
}

fn parse_uuid(value: &str) -> Result<Uuid> {
    Uuid::parse_str(value).map_err(crate::internal_error)
}

fn create_household_error_code(error: &CreateHouseholdPortError) -> Option<MutationErrorCode> {
    use CreateHouseholdPortError as Error;
    match error {
        Error::InvalidInput => Some(MutationErrorCode::BadInput),
        Error::NotFound => Some(MutationErrorCode::NotFound),
        Error::Infra => None,
    }
}

// 読み取り系はport対象外のため、postgresのエラーを直接 mapping する。
fn load_household_error_code(
    error: &postgres_household::CreateHouseholdPersistenceError,
) -> Option<MutationErrorCode> {
    use postgres_household::CreateHouseholdPersistenceError as Error;
    match error {
        Error::InvalidWorkflow(_) | Error::InvalidUuid => Some(MutationErrorCode::BadInput),
        Error::HouseholdNotFound | Error::MemberNotFound => Some(MutationErrorCode::NotFound),
        Error::Pool(_) | Error::Database(_) | Error::InvalidPersistedData => None,
    }
}

fn invitation_error_code(
    error: &postgres_invitation::HouseholdInvitationError,
) -> Option<MutationErrorCode> {
    use household_application::invitation::InvitationError as WorkflowError;
    use postgres_invitation::HouseholdInvitationError as Error;
    match error {
        Error::InvalidUuid(_) => Some(MutationErrorCode::BadInput),
        Error::Workflow(WorkflowError::InvalidIdentifier | WorkflowError::InvalidExpiry) => {
            Some(MutationErrorCode::BadInput)
        }
        Error::Workflow(WorkflowError::Forbidden) => Some(MutationErrorCode::NotFound),
        Error::Workflow(
            WorkflowError::Expired
            | WorkflowError::Revoked
            | WorkflowError::AlreadyAccepted
            | WorkflowError::AlreadyMember,
        ) => Some(MutationErrorCode::Conflict),
        Error::Workflow(WorkflowError::NotFound)
        | Error::HouseholdNotFound
        | Error::InvitationNotFound
        | Error::MemberNotFound => Some(MutationErrorCode::NotFound),
        Error::Conflict => Some(MutationErrorCode::Conflict),
        Error::InvalidCursor | Error::InvalidPagination => Some(MutationErrorCode::BadInput),
        Error::Pool(_) | Error::Database(_) => None,
    }
}

fn error_message(code: MutationErrorCode) -> &'static str {
    match code {
        MutationErrorCode::BadInput => "入力を確認してください。",
        MutationErrorCode::Conflict => "最新の状態を読み込んでから再試行してください。",
        MutationErrorCode::NotFound => "対象が見つかりません。",
        MutationErrorCode::MailForwardingDisabled => "メール転送は無効です。",
    }
}
