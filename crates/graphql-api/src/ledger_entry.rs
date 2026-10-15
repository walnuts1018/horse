use async_graphql::{ID, Interface, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{
    financial_command_result::TransferLedgerEntryResult,
    funding_command_result::FundingLedgerEntryResult,
};
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

#[derive(Clone, Debug, SimpleObject)]
pub struct AdvanceTransferLedgerEntry {
    id: ID,
    occurred_at: crate::DateTime,
    description: String,
    amount_yen: crate::Long,
    currency: String,
    created_at: crate::DateTime,
    from_member: crate::member_pool::Member,
    to_member: crate::member_pool::Member,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LoanTransferLedgerEntry {
    id: ID,
    occurred_at: crate::DateTime,
    description: String,
    amount_yen: crate::Long,
    currency: String,
    created_at: crate::DateTime,
    from_member: crate::member_pool::Member,
    to_member: crate::member_pool::Member,
    loan: LoanBalance,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RepaymentTransferLedgerEntry {
    id: ID,
    occurred_at: crate::DateTime,
    description: String,
    amount_yen: crate::Long,
    currency: String,
    created_at: crate::DateTime,
    from_member: crate::member_pool::Member,
    to_member: crate::member_pool::Member,
    loan: LoanBalance,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct GiftTransferLedgerEntry {
    id: ID,
    occurred_at: crate::DateTime,
    description: String,
    amount_yen: crate::Long,
    currency: String,
    created_at: crate::DateTime,
    from_member: crate::member_pool::Member,
    to_member: crate::member_pool::Member,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct LoanBalance {
    id: ID,
    lender_member_id: ID,
    borrower_member_id: ID,
    principal_yen: crate::Long,
    repaid_yen: crate::Long,
    balance_yen: crate::Long,
    created_at: crate::DateTime,
}

#[derive(Clone, Debug, Interface)]
#[graphql(
    field(name = "id", ty = "&ID"),
    field(name = "occurred_at", ty = "&crate::DateTime"),
    field(name = "description", ty = "&String"),
    field(name = "amount_yen", ty = "&crate::Long"),
    field(name = "currency", ty = "&String"),
    field(name = "created_at", ty = "&crate::DateTime"),
    field(name = "from_member", ty = "&crate::member_pool::Member"),
    field(name = "to_member", ty = "&crate::member_pool::Member")
)]
pub enum TransferLedgerEntry {
    AdvanceTransferLedgerEntry(AdvanceTransferLedgerEntry),
    LoanTransferLedgerEntry(LoanTransferLedgerEntry),
    RepaymentTransferLedgerEntry(RepaymentTransferLedgerEntry),
    GiftTransferLedgerEntry(GiftTransferLedgerEntry),
}

#[derive(Clone, Debug, Interface)]
#[graphql(field(name = "id", ty = "&ID"))]
pub enum Node {
    Household(crate::household_lifecycle::Household),
    PendingHouseholdInvitation(crate::household_lifecycle::PendingHouseholdInvitation),
    AcceptedHouseholdInvitation(crate::household_lifecycle::AcceptedHouseholdInvitation),
    RevokedHouseholdInvitation(crate::household_lifecycle::RevokedHouseholdInvitation),
    ExpiredHouseholdInvitation(crate::household_lifecycle::ExpiredHouseholdInvitation),
    HouseholdInvitation(crate::household_lifecycle::HouseholdInvitation),
    Member(crate::member_pool::Member),
    Pool(crate::member_pool::Pool),
    Account(crate::account_mutations::AccountObject),
    AccountMovement(crate::account_mutations::AccountMovementObject),
    AccountIncome(crate::account_mutations::AccountIncomeObject),
    Category(crate::category::Category),
    Tag(crate::catalog_mutations::Tag),
    ProductConcept(crate::catalog_mutations::ProductConcept),
    ProductVariant(crate::catalog_mutations::ProductVariant),
    ProductAlias(crate::catalog_mutations::ProductAlias),
    Purchase(crate::purchase_mutations::PurchaseObject),
    PurchaseItem(crate::purchase_mutations::PurchaseItem),
    PurchaseCorrection(crate::purchase_mutations::PurchaseCorrection),
    PurchaseWideCorrection(crate::purchase_mutations::PurchaseWideCorrection),
    PurchaseItemCorrection(crate::purchase_mutations::PurchaseItemCorrection),
    LedgerEntry(LedgerEntry),
    TransferLedgerEntry(TransferLedgerEntry),
    FundingLedgerEntry(FundingLedgerEntry),
    AdvanceTransferLedgerEntry(AdvanceTransferLedgerEntry),
    LoanTransferLedgerEntry(LoanTransferLedgerEntry),
    RepaymentTransferLedgerEntry(RepaymentTransferLedgerEntry),
    GiftTransferLedgerEntry(GiftTransferLedgerEntry),
    LoanBalance(LoanBalance),
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
    TransferLedgerEntry(TransferLedgerEntry),
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum FundingLedgerEntryMappingError {
    InvalidMemberRole,
    InvalidPoolKind,
    InvalidSettlementPolicy,
    InvalidTransferKind,
    MissingLoanBalance,
    InvalidLoanBalance,
}

impl TryFrom<TransferLedgerEntryResult> for LedgerEntry {
    type Error = FundingLedgerEntryMappingError;

    fn try_from(value: TransferLedgerEntryResult) -> Result<Self, Self::Error> {
        let id = encode_node_id("LedgerEntry", value.id);
        let occurred_at = value.occurred_at.into();
        let description = value.description;
        let amount_yen = crate::Long(value.amount_yen);
        let currency = value.currency;
        let created_at = value.created_at.into();
        let from_member = member_projection(value.from_member)?;
        let to_member = member_projection(value.to_member)?;
        let common = || {
            (
                id.clone(),
                occurred_at.clone(),
                description.clone(),
                amount_yen,
                currency.clone(),
                created_at.clone(),
                from_member.clone(),
                to_member.clone(),
            )
        };
        match value.kind.as_str() {
            "ADVANCE" => {
                let (
                    id,
                    occurred_at,
                    description,
                    amount_yen,
                    currency,
                    created_at,
                    from_member,
                    to_member,
                ) = common();
                Ok(Self::TransferLedgerEntry(
                    TransferLedgerEntry::AdvanceTransferLedgerEntry(AdvanceTransferLedgerEntry {
                        id,
                        occurred_at,
                        description,
                        amount_yen,
                        currency,
                        created_at,
                        from_member,
                        to_member,
                    }),
                ))
            }
            "LOAN" => {
                let loan = value
                    .loan
                    .ok_or(FundingLedgerEntryMappingError::MissingLoanBalance)?;
                let (
                    id,
                    occurred_at,
                    description,
                    amount_yen,
                    currency,
                    created_at,
                    from_member,
                    to_member,
                ) = common();
                Ok(Self::TransferLedgerEntry(
                    TransferLedgerEntry::LoanTransferLedgerEntry(LoanTransferLedgerEntry {
                        id,
                        occurred_at,
                        description,
                        amount_yen,
                        currency,
                        created_at,
                        from_member,
                        to_member,
                        loan: loan_balance(loan)?,
                    }),
                ))
            }
            "REPAYMENT" => {
                let loan = value
                    .loan
                    .ok_or(FundingLedgerEntryMappingError::MissingLoanBalance)?;
                let (
                    id,
                    occurred_at,
                    description,
                    amount_yen,
                    currency,
                    created_at,
                    from_member,
                    to_member,
                ) = common();
                Ok(Self::TransferLedgerEntry(
                    TransferLedgerEntry::RepaymentTransferLedgerEntry(
                        RepaymentTransferLedgerEntry {
                            id,
                            occurred_at,
                            description,
                            amount_yen,
                            currency,
                            created_at,
                            from_member,
                            to_member,
                            loan: loan_balance(loan)?,
                        },
                    ),
                ))
            }
            "GIFT" => {
                let (
                    id,
                    occurred_at,
                    description,
                    amount_yen,
                    currency,
                    created_at,
                    from_member,
                    to_member,
                ) = common();
                Ok(Self::TransferLedgerEntry(
                    TransferLedgerEntry::GiftTransferLedgerEntry(GiftTransferLedgerEntry {
                        id,
                        occurred_at,
                        description,
                        amount_yen,
                        currency,
                        created_at,
                        from_member,
                        to_member,
                    }),
                ))
            }
            _ => Err(FundingLedgerEntryMappingError::InvalidTransferKind),
        }
    }
}

fn member_projection(
    value: horse_postgres::financial_command_result::TransferMemberProjection,
) -> Result<crate::member_pool::Member, FundingLedgerEntryMappingError> {
    use crate::member_pool::{MemberProjection, MemberRole};
    let role = match value.role.as_str() {
        "OWNER" => MemberRole::Owner,
        "MEMBER" => MemberRole::Member,
        _ => return Err(FundingLedgerEntryMappingError::InvalidMemberRole),
    };
    Ok(MemberProjection {
        id: value.id,
        display_name: value.display_name,
        role,
    }
    .into())
}

fn loan_balance(
    value: horse_postgres::financial_command_result::LoanBalanceProjection,
) -> Result<LoanBalance, FundingLedgerEntryMappingError> {
    if value.principal_yen < 0 || value.repaid_yen < 0 || value.balance_yen < 0 {
        return Err(FundingLedgerEntryMappingError::InvalidLoanBalance);
    }
    Ok(LoanBalance {
        id: encode_node_id("LoanBalance", value.id),
        lender_member_id: encode_node_id("Member", value.lender_member_id),
        borrower_member_id: encode_node_id("Member", value.borrower_member_id),
        principal_yen: crate::Long(value.principal_yen),
        repaid_yen: crate::Long(value.repaid_yen),
        balance_yen: crate::Long(value.balance_yen),
        created_at: value.created_at.into(),
    })
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
