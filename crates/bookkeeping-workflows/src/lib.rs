use std::collections::BTreeMap;

use bookkeeping_domain::{AllocationShares, AllocationWeight, PoolId, PositiveYen, Yen, allocate};

pub mod account_income;
pub mod account_card_last4;
pub mod account_movement;
pub mod bulk_delete_items;
pub mod bulk_update_items;
pub mod close_period;
pub mod correct_purchase;
pub mod create_account;
pub mod create_pool;
pub mod create_purchase;
pub mod ingestion_job;
pub mod record_repayment;
pub mod record_transfer;
pub mod set_funding_schedule;
pub mod update_purchase;
pub mod update_purchase_item;

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct PurchaseId(String);

impl PurchaseId {
    pub fn new(value: impl Into<String>) -> Result<Self, IdentifierError> {
        identifier(value).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct AccountId(String);

impl AccountId {
    pub fn new(value: impl Into<String>) -> Result<Self, IdentifierError> {
        identifier(value).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct MemberId(String);

impl MemberId {
    pub fn new(value: impl Into<String>) -> Result<Self, IdentifierError> {
        identifier(value).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub struct ItemId(String);

impl ItemId {
    pub fn new(value: impl Into<String>) -> Result<Self, IdentifierError> {
        identifier(value).map(Self)
    }

    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct IdentifierError;

fn identifier(value: impl Into<String>) -> Result<String, IdentifierError> {
    let value = value.into();
    if value.is_empty() {
        return Err(IdentifierError);
    }
    Ok(value)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurchaseStatus {
    Draft,
    Posted,
    Other,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseDraft {
    pub id: PurchaseId,
    pub revision: u64,
    pub status: PurchaseStatus,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PoolAllocation {
    pub item_id: ItemId,
    pub pool_id: PoolId,
    pub amount: Yen,
    pub funding: MemberFundingBasis,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PurchaseItem {
    pub id: ItemId,
    pub amount: Yen,
}

/// 会員負担snapshotの算出に使うfunding rule情報。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MemberFundingBasis {
    rule_version: u32,
    kind: FundingBasisKind,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum FundingBasisKind {
    Personal {
        owner: MemberId,
    },
    Shared {
        weights: Vec<MemberWeight>,
        settlement_policy: SettlementPolicy,
    },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SettlementPolicy {
    BalanceToStandard,
    ReimbursePersonal,
}

impl SettlementPolicy {
    const fn as_str(self) -> &'static str {
        match self {
            Self::BalanceToStandard => "BALANCE_TO_STANDARD",
            Self::ReimbursePersonal => "REIMBURSE_PERSONAL",
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FundingBasisError {
    InvalidRuleVersion,
    InvalidMemberShares,
}

impl MemberFundingBasis {
    pub fn personal(rule_version: u32, owner: MemberId) -> Result<Self, FundingBasisError> {
        if rule_version == 0 {
            return Err(FundingBasisError::InvalidRuleVersion);
        }
        Ok(Self {
            rule_version,
            kind: FundingBasisKind::Personal { owner },
        })
    }

    pub fn shared(
        rule_version: u32,
        weights: Vec<MemberWeight>,
        settlement_policy: SettlementPolicy,
    ) -> Result<Self, FundingBasisError> {
        if rule_version == 0 {
            return Err(FundingBasisError::InvalidRuleVersion);
        }
        validate_member_weights(&weights)?;
        Ok(Self {
            rule_version,
            kind: FundingBasisKind::Shared {
                weights,
                settlement_policy,
            },
        })
    }

    fn rule_version(&self) -> u32 {
        self.rule_version
    }

    fn weights(&self) -> Vec<MemberWeight> {
        match &self.kind {
            FundingBasisKind::Personal { owner } => vec![MemberWeight {
                member_id: owner.clone(),
                weight: 1,
            }],
            FundingBasisKind::Shared { weights, .. } => weights.clone(),
        }
    }

    fn settlement_policy(&self) -> Option<SettlementPolicy> {
        match &self.kind {
            FundingBasisKind::Personal { .. } => None,
            FundingBasisKind::Shared {
                settlement_policy, ..
            } => Some(*settlement_policy),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MemberWeight {
    member_id: MemberId,
    weight: u64,
}

impl MemberWeight {
    pub fn new(member_id: MemberId, weight: u64) -> Result<Self, FundingBasisError> {
        if weight == 0 {
            return Err(FundingBasisError::InvalidMemberShares);
        }
        Ok(Self { member_id, weight })
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Payment {
    pub account_id: AccountId,
    pub amount: PositiveYen,
    pub funding: PaymentFundingBasis,
}

/// 支払口座の所属poolとpayment funding snapshotの算出に使うrule情報。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PaymentFundingBasis {
    pub pool_id: PoolId,
    pub rule: MemberFundingBasis,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseCommand {
    pub draft: PurchaseDraft,
    pub expected_revision: u64,
    pub accounting_period_open: bool,
    pub total: PositiveYen,
    pub items: Vec<PurchaseItem>,
    pub allocations: Vec<PoolAllocation>,
    pub payments: Vec<Payment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseDecision {
    pub purchase_id: PurchaseId,
    pub new_revision: u64,
    pub new_status: PurchaseStatus,
    pub journal_lines: Vec<JournalLine>,
    pub member_burden_snapshots: Vec<MemberBurdenSnapshot>,
    pub payment_funding_snapshots: Vec<PaymentFundingSnapshot>,
    pub events: Vec<DomainEvent>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct JournalLine {
    pub line_no: u32,
    pub side: Side,
    pub account_id: Option<AccountId>,
    pub pool_id: Option<PoolId>,
    pub amount: PositiveYen,
    pub memo: &'static str,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Debit,
    Credit,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MemberBurdenSnapshot {
    pub item_id: ItemId,
    pub pool_id: PoolId,
    pub member_id: MemberId,
    pub amount: Yen,
    pub funding_rule_version: u32,
    pub funding_weight: u64,
    pub settlement_policy: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PaymentFundingSnapshot {
    pub line_no: u32,
    pub account_id: AccountId,
    pub pool_id: PoolId,
    pub member_id: MemberId,
    pub funding_rule_version: u32,
    pub funding_weight: u64,
    pub settlement_policy: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DomainEvent {
    pub event_type: &'static str,
    pub entity_id: PurchaseId,
    pub revision: u64,
    pub changed_fields: Vec<&'static str>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PostPurchaseError {
    AccountingPeriodClosed,
    AmountOverflow,
    RevisionMismatch { expected: u64, actual: u64 },
    PurchaseNotDraft,
    MissingPayments,
    ItemTotalMismatch,
    ItemAllocationTotalMismatch { item_id: ItemId },
    DuplicateItemId { item_id: ItemId },
    DuplicateItemPoolAllocation { item_id: ItemId, pool_id: PoolId },
    UnknownItemAllocation { item_id: ItemId },
    AllocationTotalMismatch,
    PaymentTotalMismatch,
    EmptyAllocation,
    InvalidMemberShares { pool_id: PoolId },
    RevisionOverflow,
    JournalLineCountOverflow,
    JournalUnbalanced,
}

/// 処理本体はDB状態を参照せず、post前提を検証して永続化に必要な決定を返す。
pub fn decide_post_purchase(
    command: PostPurchaseCommand,
) -> Result<PostPurchaseDecision, PostPurchaseError> {
    if !command.accounting_period_open {
        return Err(PostPurchaseError::AccountingPeriodClosed);
    }
    if command.draft.revision != command.expected_revision {
        return Err(PostPurchaseError::RevisionMismatch {
            expected: command.expected_revision,
            actual: command.draft.revision,
        });
    }
    if command.draft.status != PurchaseStatus::Draft {
        return Err(PostPurchaseError::PurchaseNotDraft);
    }
    if command.payments.is_empty() {
        return Err(PostPurchaseError::MissingPayments);
    }
    if command.allocations.is_empty() {
        return Err(PostPurchaseError::EmptyAllocation);
    }

    let mut item_amounts = BTreeMap::<ItemId, Yen>::new();
    for item in &command.items {
        if item_amounts.insert(item.id.clone(), item.amount).is_some() {
            return Err(PostPurchaseError::DuplicateItemId {
                item_id: item.id.clone(),
            });
        }
    }
    if sum_yen(command.items.iter().map(|item| item.amount))? != command.total.yen() {
        return Err(PostPurchaseError::ItemTotalMismatch);
    }

    let mut item_allocation_totals = BTreeMap::<ItemId, Yen>::new();
    let mut item_pool_pairs = std::collections::BTreeSet::new();
    for allocation in &command.allocations {
        if !item_amounts.contains_key(&allocation.item_id) {
            return Err(PostPurchaseError::UnknownItemAllocation {
                item_id: allocation.item_id.clone(),
            });
        }
        if !item_pool_pairs.insert((allocation.item_id.clone(), allocation.pool_id.clone())) {
            return Err(PostPurchaseError::DuplicateItemPoolAllocation {
                item_id: allocation.item_id.clone(),
                pool_id: allocation.pool_id.clone(),
            });
        }
        let allocated = item_allocation_totals
            .get(&allocation.item_id)
            .copied()
            .unwrap_or(Yen::new(0).expect("0 is in range"));
        item_allocation_totals.insert(
            allocation.item_id.clone(),
            allocated
                .checked_add(allocation.amount)
                .map_err(|_| PostPurchaseError::AmountOverflow)?,
        );
    }
    for (item_id, amount) in &item_amounts {
        if item_allocation_totals
            .get(item_id)
            .copied()
            .unwrap_or(Yen::new(0).expect("0 is in range"))
            != *amount
        {
            return Err(PostPurchaseError::ItemAllocationTotalMismatch {
                item_id: item_id.clone(),
            });
        }
    }

    let allocation_total = sum_yen(
        command
            .allocations
            .iter()
            .map(|allocation| allocation.amount),
    )?;
    if allocation_total != command.total.yen() {
        return Err(PostPurchaseError::AllocationTotalMismatch);
    }
    if !command.payments.is_empty() {
        let payment_total = sum_yen(command.payments.iter().map(|payment| payment.amount.yen()))?;
        if payment_total != command.total.yen() {
            return Err(PostPurchaseError::PaymentTotalMismatch);
        }
    }

    let mut pool_totals = BTreeMap::<PoolId, Yen>::new();
    let mut member_burden_snapshots = Vec::new();
    for allocation in &command.allocations {
        let existing = pool_totals
            .get(&allocation.pool_id)
            .copied()
            .unwrap_or(Yen::new(0).expect("0 is in range"));
        pool_totals.insert(
            allocation.pool_id.clone(),
            existing
                .checked_add(allocation.amount)
                .map_err(|_| PostPurchaseError::AmountOverflow)?,
        );
        let funding_weights = allocation.funding.weights();
        let shares = shares(&allocation.pool_id, &funding_weights)?;
        for part in allocate(allocation.amount, &shares) {
            let weight = funding_weights
                .iter()
                .find(|weight| weight.member_id.as_str() == part.pool_id().as_str())
                .expect("shares preserve member ids");
            member_burden_snapshots.push(MemberBurdenSnapshot {
                item_id: allocation.item_id.clone(),
                pool_id: allocation.pool_id.clone(),
                member_id: weight.member_id.clone(),
                amount: part.amount(),
                funding_rule_version: allocation.funding.rule_version(),
                funding_weight: weight.weight,
                settlement_policy: allocation
                    .funding
                    .settlement_policy()
                    .map(|policy| policy.as_str().to_owned()),
            });
        }
    }

    let mut payment_funding_snapshots = Vec::new();
    for (index, payment) in command.payments.iter().enumerate() {
        let funding_weights = payment.funding.rule.weights();
        shares(&payment.funding.pool_id, &funding_weights)?;
        if index + 1 > u32::MAX as usize {
            return Err(PostPurchaseError::JournalLineCountOverflow);
        }
        let mut weights = funding_weights.iter().collect::<Vec<_>>();
        weights.sort_by(|left, right| left.member_id.cmp(&right.member_id));
        for weight in weights {
            payment_funding_snapshots.push(PaymentFundingSnapshot {
                line_no: (index + 1) as u32,
                account_id: payment.account_id.clone(),
                pool_id: payment.funding.pool_id.clone(),
                member_id: weight.member_id.clone(),
                funding_rule_version: payment.funding.rule.rule_version(),
                funding_weight: weight.weight,
                settlement_policy: payment
                    .funding
                    .rule
                    .settlement_policy()
                    .map(|policy| policy.as_str().to_owned()),
            });
        }
    }

    let mut journal_lines = Vec::new();
    for (pool_id, amount) in pool_totals {
        if amount.value() == 0 {
            continue;
        }
        journal_lines.push(JournalLine {
            line_no: next_line_no(journal_lines.len())?,
            side: Side::Debit,
            account_id: None,
            pool_id: Some(pool_id),
            amount: PositiveYen::new(amount.value()).expect("nonzero pool total is positive"),
            memo: "Purchase allocation",
        });
    }
    for payment in &command.payments {
        journal_lines.push(JournalLine {
            line_no: next_line_no(journal_lines.len())?,
            side: Side::Credit,
            account_id: Some(payment.account_id.clone()),
            pool_id: None,
            amount: payment.amount,
            memo: "Payment",
        });
    }
    let debit_total = sum_yen(
        journal_lines
            .iter()
            .filter(|line| line.side == Side::Debit)
            .map(|line| line.amount.yen()),
    )?;
    let credit_total = sum_yen(
        journal_lines
            .iter()
            .filter(|line| line.side == Side::Credit)
            .map(|line| line.amount.yen()),
    )?;
    if debit_total != credit_total {
        return Err(PostPurchaseError::JournalUnbalanced);
    }

    let new_revision = command
        .draft
        .revision
        .checked_add(1)
        .ok_or(PostPurchaseError::RevisionOverflow)?;
    let purchase_id = command.draft.id;
    Ok(PostPurchaseDecision {
        purchase_id: purchase_id.clone(),
        new_revision,
        new_status: PurchaseStatus::Posted,
        journal_lines,
        member_burden_snapshots,
        payment_funding_snapshots,
        events: vec![DomainEvent {
            event_type: "purchase.posted",
            entity_id: purchase_id,
            revision: new_revision,
            changed_fields: vec!["status"],
        }],
    })
}

fn shares(
    pool_id: &PoolId,
    weights: &[MemberWeight],
) -> Result<AllocationShares, PostPurchaseError> {
    let domain_weights = weights
        .iter()
        .map(|weight| {
            let member_as_pool =
                PoolId::new(weight.member_id.as_str().to_owned()).expect("member IDs are nonempty");
            AllocationWeight::new(member_as_pool, weight.weight)
        })
        .collect::<Result<Vec<_>, _>>();
    let domain_weights = domain_weights.map_err(|_| PostPurchaseError::InvalidMemberShares {
        pool_id: pool_id.clone(),
    })?;
    AllocationShares::new(domain_weights).map_err(|_| PostPurchaseError::InvalidMemberShares {
        pool_id: pool_id.clone(),
    })
}

fn validate_member_weights(weights: &[MemberWeight]) -> Result<(), FundingBasisError> {
    let domain_weights = weights
        .iter()
        .map(|weight| {
            let member_as_pool =
                PoolId::new(weight.member_id.as_str().to_owned()).expect("member IDs are nonempty");
            AllocationWeight::new(member_as_pool, weight.weight)
        })
        .collect::<Result<Vec<_>, _>>()
        .map_err(|_| FundingBasisError::InvalidMemberShares)?;
    AllocationShares::new(domain_weights)
        .map(|_| ())
        .map_err(|_| FundingBasisError::InvalidMemberShares)
}

fn sum_yen(mut values: impl Iterator<Item = Yen>) -> Result<Yen, PostPurchaseError> {
    values.try_fold(Yen::new(0).expect("0 is in range"), |total, amount| {
        total
            .checked_add(amount)
            .map_err(|_| PostPurchaseError::AmountOverflow)
    })
}

fn next_line_no(current_count: usize) -> Result<u32, PostPurchaseError> {
    u32::try_from(current_count + 1).map_err(|_| PostPurchaseError::JournalLineCountOverflow)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn id<T>(new: impl FnOnce(String) -> Result<T, IdentifierError>, value: &str) -> T {
        new(value.to_owned()).unwrap()
    }

    fn member_weight(member: &str, weight: u64) -> MemberWeight {
        MemberWeight::new(id(MemberId::new, member), weight).unwrap()
    }

    fn command() -> PostPurchaseCommand {
        let pool_id = PoolId::new("pool-a").unwrap();
        PostPurchaseCommand {
            draft: PurchaseDraft {
                id: id(PurchaseId::new, "purchase-1"),
                revision: 4,
                status: PurchaseStatus::Draft,
            },
            expected_revision: 4,
            accounting_period_open: true,
            total: PositiveYen::new(101).unwrap(),
            items: vec![PurchaseItem {
                id: id(ItemId::new, "item-1"),
                amount: Yen::new(101).unwrap(),
            }],
            allocations: vec![PoolAllocation {
                item_id: id(ItemId::new, "item-1"),
                pool_id: pool_id.clone(),
                amount: Yen::new(101).unwrap(),
                funding: MemberFundingBasis::shared(
                    2,
                    vec![member_weight("member-b", 1), member_weight("member-a", 1)],
                    SettlementPolicy::BalanceToStandard,
                )
                .unwrap(),
            }],
            payments: vec![Payment {
                account_id: id(AccountId::new, "account-1"),
                amount: PositiveYen::new(101).unwrap(),
                funding: PaymentFundingBasis {
                    pool_id,
                    rule: MemberFundingBasis::personal(2, id(MemberId::new, "member-a")).unwrap(),
                },
            }],
        }
    }

    #[test]
    fn posting_returns_balanced_deterministic_journal_and_snapshots() {
        let decision = decide_post_purchase(command()).unwrap();
        assert_eq!(decision.new_revision, 5);
        assert_eq!(decision.new_status, PurchaseStatus::Posted);
        assert_eq!(
            decision
                .journal_lines
                .iter()
                .map(|line| line.line_no)
                .collect::<Vec<_>>(),
            vec![1, 2]
        );
        assert_eq!(
            decision
                .member_burden_snapshots
                .iter()
                .map(|row| (row.member_id.as_str(), row.amount.value()))
                .collect::<Vec<_>>(),
            vec![("member-a", 51), ("member-b", 50)]
        );
        assert_eq!(decision.events[0].event_type, "purchase.posted");
    }

    #[test]
    fn posting_rejects_stale_or_unbalanced_inputs() {
        let mut stale = command();
        stale.expected_revision -= 1;
        assert!(matches!(
            decide_post_purchase(stale),
            Err(PostPurchaseError::RevisionMismatch { .. })
        ));

        let mut unbalanced = command();
        unbalanced.allocations[0].amount = Yen::new(100).unwrap();
        assert!(matches!(
            decide_post_purchase(unbalanced),
            Err(PostPurchaseError::ItemAllocationTotalMismatch { item_id })
                if item_id.as_str() == "item-1"
        ));

        let mut closed = command();
        closed.accounting_period_open = false;
        assert_eq!(
            decide_post_purchase(closed),
            Err(PostPurchaseError::AccountingPeriodClosed)
        );
    }

    #[test]
    fn posting_rejects_missing_payments_and_invalid_item_allocations() {
        let mut no_payments = command();
        no_payments.payments.clear();
        assert_eq!(
            decide_post_purchase(no_payments),
            Err(PostPurchaseError::MissingPayments)
        );

        let mut wrong_item_total = command();
        wrong_item_total.items[0].amount = Yen::new(100).unwrap();
        assert_eq!(
            decide_post_purchase(wrong_item_total),
            Err(PostPurchaseError::ItemTotalMismatch)
        );

        let mut duplicate_item = command();
        duplicate_item.items.push(duplicate_item.items[0].clone());
        assert!(matches!(
            decide_post_purchase(duplicate_item),
            Err(PostPurchaseError::DuplicateItemId { .. })
        ));

        let mut duplicate_item_pool = command();
        duplicate_item_pool
            .allocations
            .push(duplicate_item_pool.allocations[0].clone());
        assert!(matches!(
            decide_post_purchase(duplicate_item_pool),
            Err(PostPurchaseError::DuplicateItemPoolAllocation { .. })
        ));

        let mut unknown_item = command();
        unknown_item.allocations[0].item_id = id(ItemId::new, "unknown-item");
        assert!(matches!(
            decide_post_purchase(unknown_item),
            Err(PostPurchaseError::UnknownItemAllocation { .. })
        ));
    }

    #[test]
    fn posting_aggregates_journal_by_pool_and_keeps_item_snapshots_separate() {
        let mut command = command();
        command.total = PositiveYen::new(150).unwrap();
        command.items[0].amount = Yen::new(101).unwrap();
        command.allocations[0].amount = Yen::new(101).unwrap();
        let mut second_item = command.allocations[0].clone();
        second_item.item_id = id(ItemId::new, "item-2");
        second_item.amount = Yen::new(49).unwrap();
        command.allocations.push(second_item);
        command.items.push(PurchaseItem {
            id: id(ItemId::new, "item-2"),
            amount: Yen::new(49).unwrap(),
        });
        command.payments[0].amount = PositiveYen::new(75).unwrap();
        let mut second_payment = command.payments[0].clone();
        second_payment.account_id = id(AccountId::new, "account-2");
        second_payment.amount = PositiveYen::new(75).unwrap();
        command.payments.push(second_payment);

        let decision = decide_post_purchase(command).unwrap();

        assert_eq!(decision.journal_lines.len(), 3);
        assert_eq!(decision.journal_lines[0].side, Side::Debit);
        assert_eq!(decision.journal_lines[0].amount.value(), 150);
        assert_eq!(
            decision
                .member_burden_snapshots
                .iter()
                .filter(|row| row.item_id.as_str() == "item-1")
                .map(|row| row.amount.value())
                .sum::<i64>(),
            101
        );
        assert_eq!(
            decision
                .member_burden_snapshots
                .iter()
                .filter(|row| row.item_id.as_str() == "item-2")
                .map(|row| row.amount.value())
                .sum::<i64>(),
            49
        );
        assert_eq!(decision.payment_funding_snapshots[0].line_no, 1);
        assert_eq!(decision.payment_funding_snapshots[1].line_no, 2);
    }

    #[test]
    fn posting_accepts_zero_amount_allocations_for_an_item() {
        let mut command = command();
        let zero_item = id(ItemId::new, "item-zero");
        command.items.push(PurchaseItem {
            id: zero_item.clone(),
            amount: Yen::new(0).unwrap(),
        });
        let mut zero_allocation = command.allocations[0].clone();
        zero_allocation.item_id = zero_item;
        zero_allocation.amount = Yen::new(0).unwrap();
        command.allocations.push(zero_allocation);

        let decision = decide_post_purchase(command).unwrap();

        assert_eq!(decision.journal_lines.len(), 2);
        assert!(decision.member_burden_snapshots.iter().any(|snapshot| {
            snapshot.item_id.as_str() == "item-zero" && snapshot.amount.value() == 0
        }));
    }
}
