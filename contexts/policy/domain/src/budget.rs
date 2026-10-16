use chrono::{Datelike, NaiveDate};
use uuid::Uuid;

const MAX_AMOUNT_YEN: i64 = 1_000_000_000_000;

macro_rules! budget_id {
    ($name:ident) => {
        #[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
        pub struct $name(Uuid);

        impl $name {
            pub fn from_persisted(value: Uuid) -> Self {
                Self(value)
            }

            pub const fn value(self) -> Uuid {
                self.0
            }
        }
    };
}

budget_id!(BudgetActorId);
budget_id!(BudgetHouseholdId);
budget_id!(BudgetId);
budget_id!(BudgetCategoryId);

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct BudgetRevision(i64);

impl BudgetRevision {
    pub const fn value(self) -> i64 {
        self.0
    }

    pub fn from_persisted(value: i64) -> Result<Self, BudgetError> {
        Self::try_from(value)
    }

    pub fn next(self) -> Result<Self, BudgetError> {
        self.0
            .checked_add(1)
            .map(Self)
            .ok_or(BudgetError::InvalidRevision)
    }
}

impl TryFrom<i64> for BudgetRevision {
    type Error = BudgetError;

    fn try_from(value: i64) -> Result<Self, Self::Error> {
        if value > 0 {
            Ok(Self(value))
        } else {
            Err(BudgetError::InvalidRevision)
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct BudgetName(String);

impl BudgetName {
    pub fn as_str(&self) -> &str {
        &self.0
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct BudgetAmountYen(i64);

impl BudgetAmountYen {
    pub fn value(self) -> i64 {
        self.0
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Budget {
    id: BudgetId,
    household_id: BudgetHouseholdId,
    category_id: Option<BudgetCategoryId>,
    name: BudgetName,
    amount_yen: BudgetAmountYen,
    starts_on: NaiveDate,
    ends_on: Option<NaiveDate>,
    archived: bool,
    revision: BudgetRevision,
}

impl Budget {
    pub fn reconstitute(
        id: BudgetId,
        household_id: BudgetHouseholdId,
        category_id: Option<BudgetCategoryId>,
        name: impl Into<String>,
        amount_yen: i64,
        starts_on: NaiveDate,
        ends_on: Option<NaiveDate>,
        archived: bool,
        revision: i64,
    ) -> Result<Self, BudgetError> {
        validate_dates(starts_on, ends_on)?;
        Ok(Self {
            id,
            household_id,
            category_id,
            name: BudgetName(validate_name(name.into())?),
            amount_yen: BudgetAmountYen::try_from(amount_yen)?,
            starts_on,
            ends_on,
            archived,
            revision: BudgetRevision::try_from(revision)?,
        })
    }

    pub const fn id(&self) -> BudgetId {
        self.id
    }

    pub const fn household_id(&self) -> BudgetHouseholdId {
        self.household_id
    }

    pub const fn category_id(&self) -> Option<BudgetCategoryId> {
        self.category_id
    }

    pub fn name(&self) -> &BudgetName {
        &self.name
    }

    pub const fn amount_yen(&self) -> BudgetAmountYen {
        self.amount_yen
    }

    pub const fn starts_on(&self) -> NaiveDate {
        self.starts_on
    }

    pub const fn ends_on(&self) -> Option<NaiveDate> {
        self.ends_on
    }

    pub const fn archived(&self) -> bool {
        self.archived
    }

    pub const fn revision(&self) -> BudgetRevision {
        self.revision
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateBudgetCommand {
    pub actor_id: BudgetActorId,
    pub household_id: BudgetHouseholdId,
    pub id: BudgetId,
    pub category_id: Option<BudgetCategoryId>,
    pub name: String,
    pub amount_yen: i64,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateBudgetCommand {
    pub actor_id: BudgetActorId,
    pub household_id: BudgetHouseholdId,
    pub id: BudgetId,
    pub expected_revision: i64,
    pub category_id: Option<BudgetCategoryId>,
    pub name: String,
    pub amount_yen: i64,
    pub starts_on: NaiveDate,
    pub effective_from: NaiveDate,
    pub ends_on: Option<Option<NaiveDate>>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ArchiveBudgetCommand {
    pub actor_id: BudgetActorId,
    pub household_id: BudgetHouseholdId,
    pub id: BudgetId,
    pub expected_revision: i64,
    pub archived: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CreateBudgetDecision {
    pub actor_id: BudgetActorId,
    pub id: BudgetId,
    pub household_id: BudgetHouseholdId,
    pub category_id: Option<BudgetCategoryId>,
    pub name: BudgetName,
    pub amount_yen: BudgetAmountYen,
    pub starts_on: NaiveDate,
    pub ends_on: Option<NaiveDate>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EndDateChange {
    Unchanged,
    Set(Option<NaiveDate>),
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct UpdateBudgetDecision {
    pub actor_id: BudgetActorId,
    pub id: BudgetId,
    pub household_id: BudgetHouseholdId,
    pub expected_revision: BudgetRevision,
    pub category_id: Option<BudgetCategoryId>,
    pub name: BudgetName,
    pub amount_yen: BudgetAmountYen,
    pub starts_on: NaiveDate,
    pub effective_from: NaiveDate,
    pub ends_on: EndDateChange,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ArchiveBudgetDecision {
    pub actor_id: BudgetActorId,
    pub id: BudgetId,
    pub household_id: BudgetHouseholdId,
    pub expected_revision: BudgetRevision,
    pub archived: bool,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BudgetError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidId,
    InvalidCategoryId,
    InvalidName,
    InvalidAmount,
    InvalidDateRange,
    InvalidRevision,
}

pub fn decide_create(command: CreateBudgetCommand) -> Result<CreateBudgetDecision, BudgetError> {
    let category_id = command.category_id;
    let name = BudgetName(validate_name(command.name)?);
    let amount_yen = BudgetAmountYen::try_from(command.amount_yen)?;
    validate_dates(command.starts_on, command.ends_on)?;
    Ok(CreateBudgetDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: command.household_id,
        category_id,
        name,
        amount_yen,
        starts_on: command.starts_on,
        ends_on: command.ends_on,
    })
}

pub fn decide_update(command: UpdateBudgetCommand) -> Result<UpdateBudgetDecision, BudgetError> {
    let expected_revision = BudgetRevision::try_from(command.expected_revision)?;
    let category_id = command.category_id;
    let name = BudgetName(validate_name(command.name)?);
    let amount_yen = BudgetAmountYen::try_from(command.amount_yen)?;
    if command.effective_from.day() != 1 {
        return Err(BudgetError::InvalidDateRange);
    }
    if let Some(ends_on) = command.ends_on {
        validate_dates(command.starts_on, ends_on)?;
    }
    Ok(UpdateBudgetDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: command.household_id,
        expected_revision,
        category_id,
        name,
        amount_yen,
        starts_on: command.starts_on,
        effective_from: command.effective_from,
        ends_on: command
            .ends_on
            .map_or(EndDateChange::Unchanged, EndDateChange::Set),
    })
}

pub fn decide_archive(command: ArchiveBudgetCommand) -> Result<ArchiveBudgetDecision, BudgetError> {
    let expected_revision = BudgetRevision::try_from(command.expected_revision)?;
    Ok(ArchiveBudgetDecision {
        actor_id: command.actor_id,
        id: command.id,
        household_id: command.household_id,
        expected_revision,
        archived: command.archived,
    })
}

fn validate_name(value: String) -> Result<String, BudgetError> {
    let value = value.trim().to_owned();
    if value.is_empty() || value.chars().count() > 120 {
        Err(BudgetError::InvalidName)
    } else {
        Ok(value)
    }
}
impl TryFrom<i64> for BudgetAmountYen {
    type Error = BudgetError;

    fn try_from(value: i64) -> Result<Self, Self::Error> {
        if (1..=MAX_AMOUNT_YEN).contains(&value) {
            Ok(Self(value))
        } else {
            Err(BudgetError::InvalidAmount)
        }
    }
}
fn validate_dates(starts: NaiveDate, ends: Option<NaiveDate>) -> Result<(), BudgetError> {
    if ends.is_some_and(|end| end < starts) {
        Err(BudgetError::InvalidDateRange)
    } else {
        Ok(())
    }
}
