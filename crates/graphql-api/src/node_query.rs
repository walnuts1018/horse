use async_graphql::{Context, Error, ID, Result};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use chrono::NaiveDate;
use horse_postgres::{PgPool, financial_command_result, node_query as postgres_query};
use uuid::Uuid;

use crate::ledger_entry::{self, LedgerEntry};

pub async fn graphql_node(ctx: &Context<'_>, global_id: ID) -> Result<Option<ledger_entry::Node>> {
    let decoded = decode_relay_id(&global_id)?;
    match decoded.type_name.as_str() {
        "UnmatchedObservation" => {
            let (document_id, source_reference) = decode_unmatched_payload(&decoded.payload)?;
            let actor = crate::actor(ctx)?;
            let household_ids =
                postgres_query::active_household_ids(ctx.data::<PgPool>()?, actor.0)
                    .await
                    .map_err(crate::internal_error)?;
            for household_id in household_ids {
                if let Some(node) =
                    crate::unmatched_observations_query::resolve_unmatched_observation(
                        ctx,
                        household_id,
                        document_id,
                        &source_reference,
                    )
                    .await?
                {
                    return Ok(Some(match node {
                        crate::unmatched_observations_query::Observation::UnmatchedObservation(
                            value,
                        ) => ledger_entry::Node::UnmatchedObservation(value),
                        crate::unmatched_observations_query::Observation::ConfirmedObservation(
                            value,
                        ) => ledger_entry::Node::ConfirmedObservation(value),
                    }));
                }
            }
            Ok(None)
        }
        "Settlement" => resolve_settlement_node(ctx, &decoded.payload).await,
        type_name => {
            if !is_known_node_type(type_name) {
                return Err(Error::new(format!("unknown node type {type_name:?}")));
            }
            let id = decode_uuid_payload(type_name, &decoded.payload)?;
            if type_name == "Household" {
                return crate::household_query::graphql_household(
                    ctx,
                    encode_global_node_id("Household", id),
                )
                .await
                .map(|value| value.map(ledger_entry::Node::Household));
            }
            if !is_supported_node_type(type_name) {
                return Ok(None);
            }
            let actor = crate::actor(ctx)?;
            let household_ids =
                postgres_query::active_household_ids(ctx.data::<PgPool>()?, actor.0)
                    .await
                    .map_err(crate::internal_error)?;
            for household_id in household_ids {
                match resolve_graphql_node(ctx, household_id, type_name, id).await {
                    Ok(node) => return Ok(Some(node)),
                    Err(error) if error.message == "not found" => continue,
                    Err(error) if error.message.starts_with("unsupported node type") => {
                        return Ok(None);
                    }
                    Err(error) => return Err(error),
                }
            }
            Ok(None)
        }
    }
}

struct DecodedRelayId {
    type_name: String,
    payload: Vec<u8>,
}

fn decode_relay_id(global_id: &ID) -> Result<DecodedRelayId> {
    let bytes = URL_SAFE_NO_PAD
        .decode(global_id.as_str())
        .map_err(|_| Error::new("invalid node ID"))?;
    let (type_name, payload) = bytes
        .split_once(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    let type_name = std::str::from_utf8(type_name)
        .map_err(|_| Error::new("invalid node ID"))?
        .to_owned();
    Ok(DecodedRelayId {
        type_name,
        payload: payload.to_vec(),
    })
}

fn decode_uuid_payload(type_name: &str, payload: &[u8]) -> Result<Uuid> {
    if payload.len() != 16 {
        return Err(Error::new(format!("invalid {type_name} node ID payload")));
    }
    let id = Uuid::from_slice(payload)
        .map_err(|_| Error::new(format!("invalid {type_name} node ID")))?;
    if id.is_nil() {
        return Err(Error::new(format!("invalid {type_name} node ID")));
    }
    Ok(id)
}

fn decode_unmatched_payload(payload: &[u8]) -> Result<(Uuid, String)> {
    let (document_bytes, rest) = payload
        .split_once(|byte| *byte == 0)
        .ok_or_else(|| Error::new("invalid UnmatchedObservation node ID payload"))?;
    let document_id = match document_bytes.len() {
        16 => Uuid::from_slice(document_bytes),
        36 => Uuid::parse_str(
            std::str::from_utf8(document_bytes)
                .map_err(|_| Error::new("invalid UnmatchedObservation node ID payload"))?,
        ),
        _ => return Err(Error::new("invalid UnmatchedObservation node ID payload")),
    }
    .map_err(|_| Error::new("invalid UnmatchedObservation node ID payload"))?;
    if document_id.is_nil() {
        return Err(Error::new("invalid UnmatchedObservation node ID payload"));
    }
    let reference = if rest.len() >= 8
        && u64::from_be_bytes(rest[..8].try_into().expect("length slice has eight bytes"))
            == (rest.len() - 8) as u64
    {
        &rest[8..]
    } else if rest.len() >= 4
        && u32::from_be_bytes(rest[..4].try_into().expect("length slice has four bytes"))
            == (rest.len() - 4) as u32
    {
        &rest[4..]
    } else {
        return Err(Error::new("invalid UnmatchedObservation node ID payload"));
    };
    let source_reference = std::str::from_utf8(reference)
        .map_err(|_| Error::new("invalid UnmatchedObservation node ID payload"))?
        .to_owned();
    Ok((document_id, source_reference))
}

fn is_known_node_type(type_name: &str) -> bool {
    matches!(
        type_name,
        "Account"
            | "AccountIncome"
            | "AccountMovement"
            | "AccountOpeningBalance"
            | "AllocationRule"
            | "Budget"
            | "Category"
            | "FundingSchedule"
            | "Household"
            | "HouseholdInvitation"
            | "LoanBalance"
            | "Member"
            | "LedgerEntry"
            | "Pool"
            | "ProductAlias"
            | "ProductConcept"
            | "ProductVariant"
            | "Purchase"
            | "PurchaseCorrection"
            | "PurchaseItem"
            | "RecurringExpense"
            | "Tag"
    )
}

fn is_supported_node_type(type_name: &str) -> bool {
    matches!(
        type_name,
        "AccountOpeningBalance"
            | "AccountMovement"
            | "AccountIncome"
            | "Purchase"
            | "PurchaseCorrection"
            | "LedgerEntry"
    )
}

async fn resolve_settlement_node(
    ctx: &Context<'_>,
    payload: &[u8],
) -> Result<Option<ledger_entry::Node>> {
    let payload = std::str::from_utf8(payload)
        .map_err(|_| Error::new("invalid Settlement node ID payload"))?;
    let (household_id, period) = payload
        .split_once(':')
        .ok_or_else(|| Error::new("invalid Settlement node ID payload"))?;
    let household_id =
        Uuid::parse_str(household_id).map_err(|_| Error::new("invalid Settlement household ID"))?;
    if household_id.is_nil() {
        return Err(Error::new("invalid Settlement household ID"));
    }
    if period.len() != 7 || period.as_bytes().get(4) != Some(&b'-') {
        return Err(Error::new("invalid Settlement period"));
    }
    let month = NaiveDate::parse_from_str(&format!("{period}-01"), "%Y-%m-%d")
        .map_err(|_| Error::new("invalid Settlement period"))?;
    match crate::settlement_mutations::settlement(
        ctx,
        encode_global_node_id("Household", household_id),
        crate::policy_mutations::YearMonth::from_naive_date(month),
    )
    .await
    {
        Ok(crate::settlement_mutations::Settlement::OpenSettlement(value)) => {
            Ok(Some(ledger_entry::Node::OpenSettlement(value)))
        }
        Ok(crate::settlement_mutations::Settlement::ClosedSettlement(value)) => {
            Ok(Some(ledger_entry::Node::ClosedSettlement(value)))
        }
        Err(error)
            if error
                .message
                .starts_with("settlement query returned no result") =>
        {
            Ok(None)
        }
        Err(error) if error.message == "not found" => Ok(None),
        Err(error) => Err(error),
    }
}

pub async fn graphql_nodes(
    ctx: &Context<'_>,
    global_ids: Vec<ID>,
) -> Result<Vec<Option<ledger_entry::Node>>> {
    let mut result = Vec::with_capacity(global_ids.len());
    for global_id in global_ids {
        result.push(graphql_node(ctx, global_id).await?);
    }
    Ok(result)
}

pub(crate) async fn resolve_graphql_node(
    ctx: &Context<'_>,
    household_id: Uuid,
    type_name: &str,
    id: Uuid,
) -> Result<ledger_entry::Node> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    if !horse_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }

    match type_name {
        "AccountOpeningBalance" => {
            let account = postgres_query::load_account_opening_balance_node(pool, household_id, id)
                .await
                .map_err(crate::internal_error)?
                .ok_or_else(|| Error::new("not found"))?;
            let kind = match account.account_kind.as_str() {
                "BANK" => crate::account_mutations::AccountKind::Bank,
                "CASH" => crate::account_mutations::AccountKind::Cash,
                "CARD" => crate::account_mutations::AccountKind::Card,
                "OTHER" => crate::account_mutations::AccountKind::Other,
                value => {
                    return Err(crate::internal_error(format!(
                        "invalid account kind {value}"
                    )));
                }
            };
            Ok(ledger_entry::Node::AccountOpeningBalance(
                crate::account_balance_analysis::AccountOpeningBalance {
                    id: encode_global_node_id("AccountOpeningBalance", account.account_id),
                    account: crate::account_mutations::AccountObject {
                        id: encode_global_node_id("Account", account.account_id),
                        name: account.account_name,
                        kind,
                        card_last4: None,
                        pool_id: None,
                    },
                },
            ))
        }
        "Purchase" => {
            let purchase =
                crate::purchase_mutations::purchase_result(pool, household_id, &id.to_string())
                    .await?;
            Ok(ledger_entry::Node::Purchase(purchase))
        }
        "PurchaseCorrection" => {
            crate::purchase_mutations::resolve_purchase_correction_node(ctx, household_id, id).await
        }
        "AccountMovement" => {
            let projection = postgres_query::load_account_movement_node(pool, household_id, id)
                .await
                .map_err(crate::internal_error)?
                .ok_or_else(|| Error::new("not found"))?;
            let kind = match projection.kind.as_str() {
                "TRANSFER" => crate::account_mutations::AccountMovementKind::Transfer,
                "CARD_BILL_PAYMENT" => {
                    crate::account_mutations::AccountMovementKind::CardBillPayment
                }
                value => {
                    return Err(crate::internal_error(format!(
                        "invalid account movement kind {value}"
                    )));
                }
            };
            Ok(ledger_entry::Node::AccountMovement(
                crate::account_mutations::AccountMovementObject {
                    id: encode_global_node_id("AccountMovement", projection.id),
                    household_id: encode_global_node_id("Household", projection.household_id),
                    kind,
                    occurred_at: projection.occurred_at.into(),
                    from_account_id: encode_global_node_id("Account", projection.from_account_id),
                    from_account: projection.from_account.into(),
                    to_account_id: encode_global_node_id("Account", projection.to_account_id),
                    to_account: projection.to_account.into(),
                    amount_yen: crate::Long(projection.amount_yen),
                    currency: "JPY".to_owned(),
                    description: projection.description,
                    created_at: projection.created_at.into(),
                },
            ))
        }
        "AccountIncome" => {
            let projection = postgres_query::load_account_income_node(pool, household_id, id)
                .await
                .map_err(crate::internal_error)?
                .ok_or_else(|| Error::new("not found"))?;
            Ok(ledger_entry::Node::AccountIncome(
                crate::account_mutations::AccountIncomeObject {
                    id: encode_global_node_id("AccountIncome", projection.id),
                    household_id: encode_global_node_id("Household", projection.household_id),
                    account_id: encode_global_node_id("Account", projection.account_id),
                    amount_yen: crate::Long(projection.amount_yen),
                    currency: "JPY".to_owned(),
                    occurred_at: projection.occurred_at.into(),
                    description: projection.description,
                    created_at: projection.created_at.into(),
                },
            ))
        }
        "LedgerEntry"
        | "PurchaseLedgerEntry"
        | "CorrectionLedgerEntry"
        | "FundingLedgerEntry"
        | "AdvanceTransferLedgerEntry"
        | "LoanTransferLedgerEntry"
        | "RepaymentTransferLedgerEntry"
        | "GiftTransferLedgerEntry" => {
            let metadata = postgres_query::load_ledger_entry_node(pool, household_id, id)
                .await
                .map_err(crate::internal_error)?
                .ok_or_else(|| Error::new("not found"))?;
            let entry = match metadata.source_kind.as_str() {
                "PURCHASE" | "purchase" => {
                    let purchase = crate::purchase_mutations::purchase_result(
                        pool,
                        household_id,
                        &metadata.source_id.to_string(),
                    )
                    .await?;
                    ledger_entry::purchase_ledger_entry(
                        metadata.id,
                        metadata.occurred_at,
                        metadata.description,
                        metadata.amount_yen,
                        "JPY".to_owned(),
                        metadata.created_at,
                        purchase,
                    )
                }
                "CORRECTION" | "correction" => {
                    let purchase_id = metadata.source_purchase_id.ok_or_else(|| {
                        crate::internal_error("correction has no parent purchase")
                    })?;
                    let purchase = crate::purchase_mutations::purchase_result(
                        pool,
                        household_id,
                        &purchase_id.to_string(),
                    )
                    .await?;
                    ledger_entry::correction_ledger_entry(
                        metadata.id,
                        metadata.occurred_at,
                        metadata.description,
                        metadata.amount_yen,
                        "JPY".to_owned(),
                        metadata.created_at,
                        purchase,
                    )
                }
                "FUNDING" | "funding" => {
                    let projection =
                        horse_postgres::funding_command_result::load_funding_ledger_entry_result(
                            pool,
                            household_id,
                            metadata.id,
                        )
                        .await
                        .map_err(crate::internal_error)?;
                    LedgerEntry::FundingLedgerEntry(
                        ledger_entry::FundingLedgerEntry::try_from(projection)
                            .map_err(crate::internal_error)?,
                    )
                }
                "TRANSFER" | "transfer" => {
                    let value = financial_command_result::load_transfer_ledger_entry_result(
                        pool,
                        household_id,
                        metadata.id,
                    )
                    .await
                    .map_err(crate::internal_error)?;
                    LedgerEntry::try_from(value).map_err(crate::internal_error)?
                }
                value => return Err(Error::new(format!("unsupported ledger source {value}"))),
            };
            Ok(ledger_entry::Node::LedgerEntry(entry))
        }
        _ => Err(Error::new(format!("unsupported node type {type_name}"))),
    }
}

pub(crate) async fn resolve_transfer_id_as_ledger_entry(
    ctx: &Context<'_>,
    household_id: Uuid,
    transfer_id: Uuid,
) -> Result<ledger_entry::Node> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    if !horse_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }
    let value = postgres_query::load_transfer_ledger_entry_result_by_transfer_id(
        pool,
        household_id,
        transfer_id,
    )
    .await
    .map_err(crate::internal_error)?;
    LedgerEntry::try_from(value)
        .map(ledger_entry::Node::LedgerEntry)
        .map_err(crate::internal_error)
}

pub(crate) fn decode_global_node_id(value: &ID, expected_type: &str) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(value.as_str())
        .map_err(|_| Error::new("invalid node ID"))?;
    let (type_name, raw_id) = bytes
        .split_once(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    if type_name != expected_type.as_bytes() || raw_id.len() != 16 {
        return Err(Error::new("invalid node ID"));
    }
    let id = Uuid::from_slice(raw_id).map_err(|_| Error::new("invalid node ID"))?;
    if id.is_nil() {
        return Err(Error::new("invalid node ID"));
    }
    Ok(id)
}

pub(crate) fn encode_global_node_id(type_name: &str, id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(type_name.len() + 17);
    payload.extend_from_slice(type_name.as_bytes());
    payload.push(b':');
    payload.extend_from_slice(id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}

pub(crate) fn encode_ledger_entry_id(id: Uuid) -> ID {
    encode_global_node_id("LedgerEntry", id)
}

pub(crate) fn transfer_node_from_result(
    value: financial_command_result::TransferLedgerEntryResult,
) -> Result<ledger_entry::Node> {
    LedgerEntry::try_from(value)
        .map(ledger_entry::Node::LedgerEntry)
        .map_err(crate::internal_error)
}

pub(crate) fn ledger_node_type(entry: &LedgerEntry) -> &'static str {
    match entry {
        LedgerEntry::FundingLedgerEntry(_) => "FundingLedgerEntry",
        LedgerEntry::PurchaseLedgerEntry(_) => "PurchaseLedgerEntry",
        LedgerEntry::CorrectionLedgerEntry(_) => "CorrectionLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::AdvanceTransferLedgerEntry(_),
        ) => "AdvanceTransferLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::LoanTransferLedgerEntry(_),
        ) => "LoanTransferLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::RepaymentTransferLedgerEntry(_),
        ) => "RepaymentTransferLedgerEntry",
        LedgerEntry::TransferLedgerEntry(
            ledger_entry::TransferLedgerEntry::GiftTransferLedgerEntry(_),
        ) => "GiftTransferLedgerEntry",
    }
}
