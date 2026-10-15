use std::collections::BTreeMap;

use crate::{AccountingPeriod, AccountingPeriodStatus, AllocationShares, AllocationWeight, PoolId, PositiveYen, Yen, allocate};

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

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DraftPurchase {
    pub id: PurchaseId,
    pub revision: u64,
    pub total: PositiveYen,
    pub items: Vec<PurchaseItem>,
    pub allocations: Vec<PoolAllocation>,
    pub payments: Vec<Payment>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostedPurchase {
    id: PurchaseId,
    revision: u64,
    total: PositiveYen,
    items: Vec<PurchaseItem>,
    allocations: Vec<PoolAllocation>,
    payments: Vec<Payment>,
    journal: BalancedJournal,
    member_burden_snapshots: Vec<MemberBurdenSnapshot>,
    payment_funding_snapshots: Vec<PaymentFundingSnapshot>,
}

impl PostedPurchase {
    pub fn id(&self) -> &PurchaseId { &self.id }
    pub const fn revision(&self) -> u64 { self.revision }
    pub const fn total(&self) -> PositiveYen { self.total }
    pub fn items(&self) -> &[PurchaseItem] { &self.items }
    pub fn allocations(&self) -> &[PoolAllocation] { &self.allocations }
    pub fn payments(&self) -> &[Payment] { &self.payments }
    pub fn journal(&self) -> &BalancedJournal { &self.journal }
    pub fn member_burden_snapshots(&self) -> &[MemberBurdenSnapshot] { &self.member_burden_snapshots }
    pub fn payment_funding_snapshots(&self) -> &[PaymentFundingSnapshot] { &self.payment_funding_snapshots }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Purchase {
    Draft(DraftPurchase),
    Posted(PostedPurchase),
}

impl Purchase {
    pub const fn lifecycle(&self) -> PurchaseLifecycle {
        match self {
            Self::Draft(_) => PurchaseLifecycle::Draft,
            Self::Posted(_) => PurchaseLifecycle::Posted,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurchaseLifecycle {
    Draft,
    Posted,
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
    pub const fn as_str(self) -> &'static str {
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
    pub draft: DraftPurchase,
    pub expected_revision: u64,
    pub period: AccountingPeriod,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PostPurchaseDecision {
    purchase: PostedPurchase,
    period: AccountingPeriod,
    events: Vec<PurchaseEvent>,
}

impl PostPurchaseDecision {
    pub fn purchase_id(&self) -> &PurchaseId { &self.purchase.id }
    pub const fn new_revision(&self) -> u64 { self.purchase.revision }
    pub fn purchase(&self) -> &PostedPurchase { &self.purchase }
    pub fn period(&self) -> &AccountingPeriod { &self.period }
    pub fn journal(&self) -> &BalancedJournal { &self.purchase.journal }
    pub fn journal_lines(&self) -> &[JournalLine] { self.purchase.journal.entries() }
    pub fn member_burden_snapshots(&self) -> &[MemberBurdenSnapshot] { &self.purchase.member_burden_snapshots }
    pub fn payment_funding_snapshots(&self) -> &[PaymentFundingSnapshot] { &self.purchase.payment_funding_snapshots }

    pub fn events(&self) -> &[PurchaseEvent] {
        &self.events
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct JournalLine {
    line_no: u32,
    side: Side,
    target: PostingTarget,
    amount: PositiveYen,
    memo: PurchasePostingMemo,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PurchasePostingMemo {
    Allocation,
    Payment,
}

impl PurchasePostingMemo {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::Allocation => "Purchase allocation",
            Self::Payment => "Payment",
        }
    }
}

// 借方・貸方の相手先はプールか口座のどちらか一つだけを指す。両方なし・両方ありの明細は型で表現できない。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PostingTarget {
    Pool(PoolId),
    Account(AccountId),
}

impl JournalLine {
    pub const fn pool_line(
        line_no: u32,
        side: Side,
        pool_id: PoolId,
        amount: PositiveYen,
        memo: PurchasePostingMemo,
    ) -> Self {
        Self {
            line_no,
            side,
            target: PostingTarget::Pool(pool_id),
            amount,
            memo,
        }
    }

    pub const fn account_line(
        line_no: u32,
        side: Side,
        account_id: AccountId,
        amount: PositiveYen,
        memo: PurchasePostingMemo,
    ) -> Self {
        Self {
            line_no,
            side,
            target: PostingTarget::Account(account_id),
            amount,
            memo,
        }
    }

    pub const fn line_no(&self) -> u32 {
        self.line_no
    }

    pub const fn side(&self) -> Side {
        self.side
    }

    pub const fn target(&self) -> &PostingTarget {
        &self.target
    }

    pub const fn amount(&self) -> PositiveYen {
        self.amount
    }

    pub const fn memo(&self) -> &'static str {
        self.memo.as_str()
    }
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
    pub settlement_policy: Option<SettlementPolicy>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PaymentFundingSnapshot {
    pub line_no: u32,
    pub account_id: AccountId,
    pub pool_id: PoolId,
    pub member_id: MemberId,
    pub funding_rule_version: u32,
    pub funding_weight: u64,
    pub settlement_policy: Option<SettlementPolicy>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PurchaseEvent {
    Posted { purchase_id: PurchaseId, revision: u64 },
}

impl PurchaseEvent {
    pub const fn event_type(&self) -> &'static str { "purchase.posted" }
    pub fn purchase_id(&self) -> &PurchaseId {
        match self { Self::Posted { purchase_id, .. } => purchase_id }
    }
    pub const fn revision(&self) -> u64 {
        match self { Self::Posted { revision, .. } => *revision }
    }
    pub const fn changed_fields(&self) -> &'static [&'static str] { &["status"] }
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
    if command.period.status() != AccountingPeriodStatus::Open {
        return Err(PostPurchaseError::AccountingPeriodClosed);
    }
    if command.draft.revision != command.expected_revision {
        return Err(PostPurchaseError::RevisionMismatch {
            expected: command.expected_revision,
            actual: command.draft.revision,
        });
    }
    if command.draft.payments.is_empty() {
        return Err(PostPurchaseError::MissingPayments);
    }
    if command.draft.allocations.is_empty() {
        return Err(PostPurchaseError::EmptyAllocation);
    }

    let mut item_amounts = BTreeMap::<ItemId, Yen>::new();
    for item in &command.draft.items {
        if item_amounts.insert(item.id.clone(), item.amount).is_some() {
            return Err(PostPurchaseError::DuplicateItemId {
                item_id: item.id.clone(),
            });
        }
    }
    if sum_yen(command.draft.items.iter().map(|item| item.amount))? != command.draft.total.yen() {
        return Err(PostPurchaseError::ItemTotalMismatch);
    }

    let mut item_allocation_totals = BTreeMap::<ItemId, Yen>::new();
    let mut item_pool_pairs = std::collections::BTreeSet::new();
    for allocation in &command.draft.allocations {
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
            .draft
            .allocations
            .iter()
            .map(|allocation| allocation.amount),
    )?;
    if allocation_total != command.draft.total.yen() {
        return Err(PostPurchaseError::AllocationTotalMismatch);
    }
    if !command.draft.payments.is_empty() {
        let payment_total = sum_yen(command.draft.payments.iter().map(|payment| payment.amount.yen()))?;
        if payment_total != command.draft.total.yen() {
            return Err(PostPurchaseError::PaymentTotalMismatch);
        }
    }

    let mut pool_totals = BTreeMap::<PoolId, Yen>::new();
    let mut member_burden_snapshots = Vec::new();
    for allocation in &command.draft.allocations {
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
        let shares = member_shares(&allocation.pool_id, &funding_weights)?;
        for part in allocate(allocation.amount, &shares) {
            let weight = funding_weights
                .iter()
                .find(|weight| weight.member_id == *part.id())
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
    for (index, payment) in command.draft.payments.iter().enumerate() {
        let funding_weights = payment.funding.rule.weights();
        member_shares(&payment.funding.pool_id, &funding_weights)?;
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
        journal_lines.push(JournalLine::pool_line(
            next_line_no(journal_lines.len())?,
            Side::Debit,
            pool_id,
            PositiveYen::new(amount.value()).expect("nonzero pool total is positive"),
            PurchasePostingMemo::Allocation,
        ));
    }
    for payment in &command.draft.payments {
        journal_lines.push(JournalLine::account_line(
            next_line_no(journal_lines.len())?,
            Side::Credit,
            payment.account_id.clone(),
            payment.amount,
            PurchasePostingMemo::Payment,
        ));
    }
    let debit_total = sum_yen(
        journal_lines
            .iter()
            .filter(|line| line.side() == Side::Debit)
            .map(|line| line.amount().yen()),
    )?;
    let credit_total = sum_yen(
        journal_lines
            .iter()
            .filter(|line| line.side() == Side::Credit)
            .map(|line| line.amount().yen()),
    )?;
    if debit_total != credit_total {
        return Err(PostPurchaseError::JournalUnbalanced);
    }

    let journal = BalancedJournal::new(journal_lines)
        .map_err(|_| PostPurchaseError::JournalUnbalanced)?;
    let new_revision = command.draft.revision.checked_add(1)
        .ok_or(PostPurchaseError::RevisionOverflow)?;
    let purchase_id = command.draft.id.clone();
    let purchase = PostedPurchase {
        id: command.draft.id,
        revision: new_revision,
        total: command.draft.total,
        items: command.draft.items,
        allocations: command.draft.allocations,
        payments: command.draft.payments,
        journal,
        member_burden_snapshots,
        payment_funding_snapshots,
    };
    Ok(PostPurchaseDecision {
        purchase,
        period: command.period,
        events: vec![PurchaseEvent::Posted { purchase_id, revision: new_revision }],
    })
}

fn member_shares(
    pool_id: &PoolId,
    weights: &[MemberWeight],
) -> Result<AllocationShares<MemberId>, PostPurchaseError> {
    let domain_weights = weights
        .iter()
        .map(|weight| AllocationWeight::new(weight.member_id.clone(), weight.weight))
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
        .map(|weight| AllocationWeight::new(weight.member_id.clone(), weight.weight))
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
