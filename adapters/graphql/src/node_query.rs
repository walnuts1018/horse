use async_graphql::{Context, Error, ID, Result};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_postgres::financial_command_result;
use chrono::NaiveDate;
use cross_context_postgres::node_query as postgres_query;
use horse_postgres_common::PgPool;
use std::collections::HashMap;
use uuid::Uuid;

use crate::ledger_entry::{self, LedgerEntry};

pub async fn graphql_node(ctx: &Context<'_>, global_id: ID) -> Result<Option<ledger_entry::Node>> {
    graphql_node_with_households(ctx, global_id, None, None, None, None).await
}

async fn graphql_node_with_households(
    ctx: &Context<'_>,
    global_id: ID,
    known_households: Option<&[Uuid]>,
    preloaded_household_nodes: Option<&HashMap<(String, Uuid), ledger_entry::Node>>,
    preloaded_settlements: Option<&HashMap<(Uuid, String), ledger_entry::Node>>,
    preloaded_observations: Option<&HashMap<(Uuid, String), ledger_entry::Node>>,
) -> Result<Option<ledger_entry::Node>> {
    let decoded = decode_relay_id(&global_id)?;
    match decoded.type_name.as_str() {
        "UnmatchedObservation" => {
            let (document_id, source_reference) = decode_unmatched_payload(&decoded.payload)?;
            if let Some(observations) = preloaded_observations {
                return Ok(observations.get(&(document_id, source_reference)).cloned());
            }
            let household_ids = match known_households {
                Some(household_ids) => household_ids.to_vec(),
                None => {
                    let actor = crate::actor(ctx)?;
                    postgres_query::active_household_ids(ctx.data::<PgPool>()?, actor.0)
                        .await
                        .map_err(crate::internal_error)?
                }
            };
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
                    return Ok(Some(ledger_entry::Node::Observation(node)));
                }
            }
            Ok(None)
        }
        "Settlement" => {
            if let Some(settlements) = preloaded_settlements {
                let key = decode_settlement_payload(&decoded.payload)?;
                return Ok(settlements.get(&key).cloned());
            }
            resolve_settlement_node(ctx, &decoded.payload).await
        }
        type_name => {
            if !is_known_node_type(type_name) {
                return Err(Error::new(format!("unknown node type {type_name:?}")));
            }
            let id = decode_uuid_payload(type_name, &decoded.payload)?;
            if type_name == "Household"
                && let Some(node) = preloaded_household_nodes
                    .and_then(|nodes| nodes.get(&(type_name.to_owned(), id)))
            {
                return Ok(Some(node.clone()));
            }
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
            if let Some(nodes) = preloaded_household_nodes
                && is_batched_node_type(type_name)
            {
                return Ok(nodes.get(&(type_name.to_owned(), id)).cloned());
            }
            let household_ids = match known_households {
                Some(household_ids) => household_ids.to_vec(),
                None => {
                    let actor = crate::actor(ctx)?;
                    postgres_query::active_household_ids(ctx.data::<PgPool>()?, actor.0)
                        .await
                        .map_err(crate::internal_error)?
                }
            };
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
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    let (type_name, payload) = bytes.split_at(separator);
    let payload = &payload[1..];
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
    let separator = payload
        .iter()
        .position(|byte| *byte == 0)
        .ok_or_else(|| Error::new("invalid UnmatchedObservation node ID payload"))?;
    let (document_bytes, rest) = payload.split_at(separator);
    let rest = &rest[1..];
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
            | "Account"
            | "Category"
            | "Member"
            | "Pool"
            | "FundingSchedule"
            | "Budget"
            | "LoanBalance"
            | "AllocationRule"
            | "HouseholdInvitation"
            | "ProductAlias"
            | "ProductConcept"
            | "ProductVariant"
            | "Purchase"
            | "PurchaseCorrection"
            | "PurchaseItem"
            | "RecurringExpense"
            | "Tag"
            | "LedgerEntry"
    )
}

async fn resolve_settlement_node(
    ctx: &Context<'_>,
    payload: &[u8],
) -> Result<Option<ledger_entry::Node>> {
    let (household_id, period) = decode_settlement_payload(payload)?;
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

fn decode_settlement_payload(payload: &[u8]) -> Result<(Uuid, String)> {
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
    NaiveDate::parse_from_str(&format!("{period}-01"), "%Y-%m-%d")
        .map_err(|_| Error::new("invalid Settlement period"))?;
    Ok((household_id, period.to_owned()))
}

fn map_settlement_node(settlement: crate::settlement_mutations::Settlement) -> ledger_entry::Node {
    match settlement {
        crate::settlement_mutations::Settlement::OpenSettlement(value) => {
            ledger_entry::Node::OpenSettlement(value)
        }
        crate::settlement_mutations::Settlement::ClosedSettlement(value) => {
            ledger_entry::Node::ClosedSettlement(value)
        }
    }
}

pub async fn graphql_nodes(
    ctx: &Context<'_>,
    global_ids: Vec<ID>,
) -> Result<Vec<Option<ledger_entry::Node>>> {
    if global_ids.is_empty() {
        return Ok(Vec::new());
    }
    for global_id in &global_ids {
        let decoded = decode_relay_id(global_id)?;
        match decoded.type_name.as_str() {
            "Settlement" => {
                decode_settlement_payload(&decoded.payload)?;
            }
            "UnmatchedObservation" => {
                decode_unmatched_payload(&decoded.payload)?;
            }
            type_name => {
                if !is_known_node_type(type_name) {
                    return Err(Error::new(format!("unknown node type {type_name:?}")));
                }
                decode_uuid_payload(type_name, &decoded.payload)?;
            }
        }
    }
    let actor = crate::actor(ctx)?;
    let household_ids = postgres_query::active_household_ids(ctx.data::<PgPool>()?, actor.0)
        .await
        .map_err(crate::internal_error)?;
    let mut household_node_ids = Vec::new();
    let mut settlement_keys = Vec::new();
    let mut observation_keys = Vec::new();
    for global_id in &global_ids {
        let decoded = decode_relay_id(global_id)?;
        if decoded.type_name == "Settlement" {
            let key = decode_settlement_payload(&decoded.payload)?;
            if !household_ids.contains(&key.0) {
                return Err(Error::new("not found"));
            }
            if !settlement_keys.contains(&key) {
                settlement_keys.push(key);
            }
            continue;
        }
        if decoded.type_name == "UnmatchedObservation" {
            let key = decode_unmatched_payload(&decoded.payload)?;
            if !observation_keys.contains(&key) {
                observation_keys.push(key);
            }
            continue;
        }
        if matches!(
            decoded.type_name.as_str(),
            "Account"
                | "Category"
                | "Member"
                | "Pool"
                | "AccountOpeningBalance"
                | "AccountMovement"
                | "AccountIncome"
                | "FundingSchedule"
                | "Budget"
                | "LoanBalance"
                | "AllocationRule"
                | "HouseholdInvitation"
                | "ProductAlias"
                | "ProductConcept"
                | "ProductVariant"
                | "RecurringExpense"
                | "Tag"
                | "LedgerEntry"
                | "Purchase"
                | "PurchaseItem"
                | "PurchaseCorrection"
                | "Household"
        ) {
            let id = decode_uuid_payload(&decoded.type_name, &decoded.payload)?;
            household_node_ids.push((decoded.type_name, id));
        }
    }
    let mut household_nodes = HashMap::new();
    let household_types = household_node_ids
        .iter()
        .filter(|(type_name, _)| {
            matches!(
                type_name.as_str(),
                "Account" | "Category" | "Member" | "Pool"
            )
        })
        .collect::<Vec<_>>();
    if !household_types.is_empty() {
        for household_id in &household_ids {
            let household = crate::household_lifecycle::load_household_graphql(
                ctx.data::<PgPool>()?,
                actor.0,
                *household_id,
            )
            .await?;
            for (type_name, id) in &household_types {
                if let Some(node) = crate::household_lifecycle::resolve_household_node_from_model(
                    &household, type_name, *id,
                ) {
                    household_nodes
                        .entry(((*type_name).clone(), *id))
                        .or_insert(node);
                }
            }
        }
    }
    for (id, account) in postgres_query::load_account_opening_balance_nodes(
        ctx.data::<PgPool>()?,
        &household_ids,
        &requested_ids(&household_node_ids, "AccountOpeningBalance"),
    )
    .await
    .map_err(crate::internal_error)?
    {
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
        household_nodes
            .entry(("AccountOpeningBalance".to_owned(), id))
            .or_insert(ledger_entry::Node::AccountOpeningBalance(
                crate::account_balance_analysis::AccountOpeningBalance {
                    id: encode_global_node_id("AccountOpeningBalance", id),
                    account: crate::account_mutations::AccountObject {
                        id: encode_global_node_id("Account", id),
                        name: account.account_name,
                        kind,
                        card_last4: account.card_last4,
                        pool_id: account.pool_id.map(|id| encode_global_node_id("Pool", id)),
                    },
                },
            ));
    }
    for (id, value) in postgres_query::load_account_movement_nodes(
        ctx.data::<PgPool>()?,
        &household_ids,
        &requested_ids(&household_node_ids, "AccountMovement"),
    )
    .await
    .map_err(crate::internal_error)?
    {
        let kind = match value.kind.as_str() {
            "TRANSFER" => crate::account_mutations::AccountMovementKind::Transfer,
            "CARD_BILL_PAYMENT" => crate::account_mutations::AccountMovementKind::CardBillPayment,
            other => {
                return Err(crate::internal_error(format!(
                    "invalid account movement kind {other}"
                )));
            }
        };
        household_nodes
            .entry(("AccountMovement".to_owned(), id))
            .or_insert(ledger_entry::Node::AccountMovement(
                crate::account_mutations::AccountMovementObject {
                    id: encode_global_node_id("AccountMovement", value.id),
                    household_id: encode_global_node_id("Household", value.household_id),
                    kind,
                    occurred_at: value.occurred_at.into(),
                    from_account_id: encode_global_node_id("Account", value.from_account_id),
                    from_account: value.from_account.into(),
                    to_account_id: encode_global_node_id("Account", value.to_account_id),
                    to_account: value.to_account.into(),
                    amount_yen: crate::Long(value.amount_yen),
                    currency: "JPY".to_owned(),
                    description: value.description,
                    created_at: value.created_at.into(),
                },
            ));
    }
    for (id, value) in postgres_query::load_account_income_nodes(
        ctx.data::<PgPool>()?,
        &household_ids,
        &requested_ids(&household_node_ids, "AccountIncome"),
    )
    .await
    .map_err(crate::internal_error)?
    {
        household_nodes
            .entry(("AccountIncome".to_owned(), id))
            .or_insert(ledger_entry::Node::AccountIncome(
                crate::account_mutations::AccountIncomeObject {
                    id: encode_global_node_id("AccountIncome", value.id),
                    household_id: encode_global_node_id("Household", value.household_id),
                    account_id: encode_global_node_id("Account", value.account_id),
                    amount_yen: crate::Long(value.amount_yen),
                    currency: "JPY".to_owned(),
                    occurred_at: value.occurred_at.into(),
                    description: value.description,
                    created_at: value.created_at.into(),
                },
            ));
    }
    let financial_batches = [
        (
            "FundingSchedule",
            crate::node_financial_batch::load_funding_schedules(
                ctx.data::<PgPool>()?,
                &household_ids,
                &requested_ids(&household_node_ids, "FundingSchedule"),
            )
            .await?,
        ),
        (
            "Budget",
            crate::node_financial_batch::load_budgets(
                ctx.data::<PgPool>()?,
                &household_ids,
                &requested_ids(&household_node_ids, "Budget"),
            )
            .await?,
        ),
        (
            "LoanBalance",
            crate::node_financial_batch::load_loan_balances(
                ctx.data::<PgPool>()?,
                &household_ids,
                &requested_ids(&household_node_ids, "LoanBalance"),
            )
            .await?,
        ),
    ];
    for (type_name, nodes) in financial_batches {
        household_nodes.extend(
            nodes
                .into_iter()
                .map(|(id, node)| ((type_name.to_owned(), id), node)),
        );
    }
    let household_ids_requested = requested_ids(&household_node_ids, "Household");
    if !household_ids_requested.is_empty() {
        for household in
            crate::household_query::load_households_for_actor(ctx.data::<PgPool>()?, actor.0)
                .await?
        {
            let Ok(id) = decode_global_node_id(&household.id, "Household") else {
                continue;
            };
            if household_ids_requested.contains(&id) {
                household_nodes.insert(
                    ("Household".to_owned(), id),
                    ledger_entry::Node::Household(household),
                );
            }
        }
    }
    household_nodes.extend(
        crate::node_batch_catalog_planning::load(ctx, &household_ids, &household_node_ids).await?,
    );
    household_nodes.extend(
        crate::node_ledger_entry_batch::load(
            ctx,
            &household_ids,
            &requested_ids(&household_node_ids, "LedgerEntry"),
        )
        .await?
        .into_iter()
        .map(|(id, node)| (("LedgerEntry".to_owned(), id), node)),
    );
    household_nodes
        .extend(crate::node_purchase_batch::load(ctx, &household_ids, &household_node_ids).await?);
    let settlements = crate::settlement_mutations::load_settlements_for_nodes(
        ctx.data::<PgPool>()?,
        &settlement_keys,
    )
    .await?
    .into_iter()
    .map(|(key, settlement)| (key, map_settlement_node(settlement)))
    .collect::<HashMap<_, _>>();
    let observations =
        crate::node_observation_batch::load(ctx, &household_ids, &observation_keys).await?;
    let mut result = Vec::with_capacity(global_ids.len());
    for global_id in global_ids {
        result.push(
            graphql_node_with_households(
                ctx,
                global_id,
                Some(&household_ids),
                Some(&household_nodes),
                Some(&settlements),
                Some(&observations),
            )
            .await?,
        );
    }
    Ok(result)
}

fn requested_ids(requested: &[(String, Uuid)], type_name: &str) -> Vec<Uuid> {
    let mut ids = Vec::new();
    for (requested_type, id) in requested {
        if requested_type == type_name && !ids.contains(id) {
            ids.push(*id);
        }
    }
    ids
}

fn is_batched_node_type(type_name: &str) -> bool {
    matches!(
        type_name,
        "Account"
            | "Category"
            | "Member"
            | "Pool"
            | "AccountOpeningBalance"
            | "AccountMovement"
            | "AccountIncome"
            | "FundingSchedule"
            | "Budget"
            | "LoanBalance"
            | "AllocationRule"
            | "HouseholdInvitation"
            | "ProductAlias"
            | "ProductConcept"
            | "ProductVariant"
            | "RecurringExpense"
            | "Tag"
            | "LedgerEntry"
            | "Purchase"
            | "PurchaseItem"
            | "PurchaseCorrection"
            | "Household"
    )
}

pub(crate) async fn resolve_graphql_node(
    ctx: &Context<'_>,
    household_id: Uuid,
    type_name: &str,
    id: Uuid,
) -> Result<ledger_entry::Node> {
    let actor = crate::actor(ctx)?;
    let pool = ctx.data::<PgPool>()?;
    if !household_postgres::household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }

    match type_name {
        "FundingSchedule" => {
            crate::node_financial_batch::load_funding_schedules(pool, &[household_id], &[id])
                .await?
                .remove(&id)
                .ok_or_else(|| Error::new("not found"))
        }
        "Budget" => crate::node_financial_batch::load_budgets(pool, &[household_id], &[id])
            .await?
            .remove(&id)
            .ok_or_else(|| Error::new("not found")),
        "LoanBalance" => {
            crate::node_financial_batch::load_loan_balances(pool, &[household_id], &[id])
                .await?
                .remove(&id)
                .ok_or_else(|| Error::new("not found"))
        }
        "AllocationRule"
        | "HouseholdInvitation"
        | "ProductAlias"
        | "ProductConcept"
        | "ProductVariant"
        | "RecurringExpense"
        | "Tag" => crate::node_batch_catalog_planning::load(
            ctx,
            &[household_id],
            &[(type_name.to_owned(), id)],
        )
        .await?
        .remove(&(type_name.to_owned(), id))
        .ok_or_else(|| Error::new("not found")),
        "Account" | "Category" | "Member" | "Pool" => {
            crate::household_lifecycle::resolve_household_node(
                pool,
                actor.0,
                household_id,
                type_name,
                id,
            )
            .await?
            .ok_or_else(|| Error::new("not found"))
        }
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
                        card_last4: account.card_last4,
                        pool_id: account.pool_id.map(|id| encode_global_node_id("Pool", id)),
                    },
                },
            ))
        }
        "Purchase" | "PurchaseItem" | "PurchaseCorrection" => {
            crate::node_purchase_batch::load(ctx, &[household_id], &[(type_name.to_owned(), id)])
                .await?
                .remove(&(type_name.to_owned(), id))
                .ok_or_else(|| Error::new("not found"))
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
                        bookkeeping_postgres::funding_command_result::load_funding_ledger_entry_result(
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
    if !household_postgres::household_access::is_active_member(pool, household_id, actor.0)
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
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid node ID"))?;
    let (type_name, raw_id) = bytes.split_at(separator);
    let raw_id = &raw_id[1..];
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
