#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetFundingScheduleCommand {
    pub actor_id: String,
    pub household_id: String,
    pub member_id: String,
    pub pool_id: String,
    pub schedule_id: String,
    pub amount_yen: i64,
    pub cadence: String,
    pub starts_on: String,
    pub ends_on: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ActiveFundingSchedule {
    pub id: String,
    pub starts_on: String,
    pub ends_on: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundingScheduleUpdate {
    pub id: String,
    pub ends_on: Option<String>,
    pub active: bool,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetFundingScheduleDecision {
    pub actor_id: String,
    pub household_id: String,
    pub member_id: String,
    pub pool_id: String,
    pub schedule_id: String,
    pub amount_yen: i64,
    pub cadence: String,
    pub starts_on: String,
    pub ends_on: Option<String>,
    pub prior_updates: Vec<FundingScheduleUpdate>,
    pub changed_fields: Vec<String>,
}

impl SetFundingScheduleDecision {
    pub fn ui_change(&self) -> UiChangeDescriptor {
        UiChangeDescriptor::new(
            "funding.schedule.updated",
            "fundingSchedule",
            self.changed_fields.clone(),
        )
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SetFundingScheduleError {
    InvalidActorId,
    InvalidHouseholdId,
    InvalidMemberId,
    InvalidPoolId,
    InvalidScheduleId,
    InvalidAmount,
    InvalidCadence,
    InvalidStartDate,
    InvalidEndDate,
    DuplicateActiveSchedule,
    InvalidExistingSchedule,
}

pub fn decide_set_funding_schedule(
    command: SetFundingScheduleCommand,
    active_schedules: Vec<ActiveFundingSchedule>,
) -> Result<SetFundingScheduleDecision, SetFundingScheduleError> {
    if command.actor_id.is_empty() {
        return Err(SetFundingScheduleError::InvalidActorId);
    }
    if command.household_id.is_empty() {
        return Err(SetFundingScheduleError::InvalidHouseholdId);
    }
    if command.member_id.is_empty() {
        return Err(SetFundingScheduleError::InvalidMemberId);
    }
    if command.pool_id.is_empty() {
        return Err(SetFundingScheduleError::InvalidPoolId);
    }
    if command.schedule_id.is_empty() {
        return Err(SetFundingScheduleError::InvalidScheduleId);
    }
    if !(0..=1_000_000_000_000).contains(&command.amount_yen) {
        return Err(SetFundingScheduleError::InvalidAmount);
    }
    if command.cadence != "MONTHLY" {
        return Err(SetFundingScheduleError::InvalidCadence);
    }
    let starts_on =
        parse_date(&command.starts_on).ok_or(SetFundingScheduleError::InvalidStartDate)?;
    let ends_on = command
        .ends_on
        .as_deref()
        .map(|value| parse_date(value).ok_or(SetFundingScheduleError::InvalidEndDate))
        .transpose()?;
    if ends_on.is_some_and(|end| end < starts_on) {
        return Err(SetFundingScheduleError::InvalidEndDate);
    }
    if active_schedules.len() > 1 {
        return Err(SetFundingScheduleError::DuplicateActiveSchedule);
    }

    let mut prior_updates = Vec::with_capacity(active_schedules.len());
    for schedule in active_schedules {
        if schedule.id.is_empty() {
            return Err(SetFundingScheduleError::InvalidExistingSchedule);
        }
        let prior_start = parse_date(&schedule.starts_on)
            .ok_or(SetFundingScheduleError::InvalidExistingSchedule)?;
        let prior_end = match schedule.ends_on.as_deref() {
            Some(value) => {
                Some(parse_date(value).ok_or(SetFundingScheduleError::InvalidExistingSchedule)?)
            }
            None => None,
        };
        if prior_end.is_some_and(|end| end < prior_start) {
            return Err(SetFundingScheduleError::InvalidExistingSchedule);
        }

        let adjusted_end = if prior_start < starts_on {
            let day_before = previous_day(starts_on);
            Some(prior_end.map_or(day_before, |end| end.min(day_before)))
        } else {
            prior_end
        };
        prior_updates.push(FundingScheduleUpdate {
            id: schedule.id,
            ends_on: adjusted_end.map(format_date),
            active: false,
        });
    }

    Ok(SetFundingScheduleDecision {
        actor_id: command.actor_id,
        household_id: command.household_id,
        member_id: command.member_id,
        pool_id: command.pool_id,
        schedule_id: command.schedule_id,
        amount_yen: command.amount_yen,
        cadence: command.cadence,
        starts_on: command.starts_on,
        ends_on: command.ends_on,
        prior_updates,
        changed_fields: vec![
            "amountYen".to_owned(),
            "startsOn".to_owned(),
            "endsOn".to_owned(),
        ],
    })
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
struct Date {
    year: i32,
    month: u32,
    day: u32,
}

fn parse_date(value: &str) -> Option<Date> {
    let mut parts = value.split('-');
    let year = parts.next()?.parse::<i32>().ok()?;
    let month = parts.next()?.parse::<u32>().ok()?;
    let day = parts.next()?.parse::<u32>().ok()?;
    if parts.next().is_some() || year < 1 || !(1..=12).contains(&month) {
        return None;
    }
    let month_length = match month {
        2 if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
        2 => 28,
        4 | 6 | 9 | 11 => 30,
        _ => 31,
    };
    if !(1..=month_length).contains(&day) || format_date(Date { year, month, day }) != value {
        return None;
    }
    Some(Date { year, month, day })
}

fn format_date(date: Date) -> String {
    format!("{:04}-{:02}-{:02}", date.year, date.month, date.day)
}

fn previous_day(date: Date) -> Date {
    if date.day > 1 {
        return Date {
            day: date.day - 1,
            ..date
        };
    }
    let (year, month) = if date.month == 1 {
        (date.year - 1, 12)
    } else {
        (date.year, date.month - 1)
    };
    let day = match month {
        2 if year % 4 == 0 && (year % 100 != 0 || year % 400 == 0) => 29,
        2 => 28,
        4 | 6 | 9 | 11 => 30,
        _ => 31,
    };
    Date { year, month, day }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn command(starts_on: &str) -> SetFundingScheduleCommand {
        SetFundingScheduleCommand {
            actor_id: "actor".to_owned(),
            household_id: "household".to_owned(),
            member_id: "member".to_owned(),
            pool_id: "pool".to_owned(),
            schedule_id: "new-schedule".to_owned(),
            amount_yen: 100,
            cadence: "MONTHLY".to_owned(),
            starts_on: starts_on.to_owned(),
            ends_on: None,
        }
    }

    #[test]
    fn closes_previous_active_schedule_before_new_start() {
        let decision = decide_set_funding_schedule(
            command("2024-03-01"),
            vec![ActiveFundingSchedule {
                id: "old-schedule".to_owned(),
                starts_on: "2024-01-01".to_owned(),
                ends_on: None,
            }],
        )
        .expect("valid schedule");

        assert_eq!(
            decision.prior_updates[0].ends_on.as_deref(),
            Some("2024-02-29")
        );
        assert!(!decision.prior_updates[0].active);
        assert_eq!(decision.changed_fields, ["amountYen", "startsOn", "endsOn"]);
    }

    #[test]
    fn deactivates_future_active_schedule_without_changing_its_range() {
        let decision = decide_set_funding_schedule(
            command("2024-02-01"),
            vec![ActiveFundingSchedule {
                id: "future-schedule".to_owned(),
                starts_on: "2024-03-01".to_owned(),
                ends_on: Some("2024-04-30".to_owned()),
            }],
        )
        .expect("valid schedule");

        assert_eq!(
            decision.prior_updates[0].ends_on.as_deref(),
            Some("2024-04-30")
        );
        assert!(!decision.prior_updates[0].active);
    }

    #[test]
    fn rejects_non_monthly_or_invalid_date_ranges() {
        let mut invalid = command("2024-02-30");
        assert_eq!(
            decide_set_funding_schedule(invalid.clone(), vec![]),
            Err(SetFundingScheduleError::InvalidStartDate)
        );
        invalid = command("2024-02-01");
        invalid.ends_on = Some("2024-01-31".to_owned());
        assert_eq!(
            decide_set_funding_schedule(invalid, vec![]),
            Err(SetFundingScheduleError::InvalidEndDate)
        );
        let mut invalid = command("2024-02-01");
        invalid.cadence = "WEEKLY".to_owned();
        assert_eq!(
            decide_set_funding_schedule(invalid, vec![]),
            Err(SetFundingScheduleError::InvalidCadence)
        );
    }
}

use horse_household_change::UiChangeDescriptor;
