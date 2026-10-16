use crate::schema::public;
use bookkeeping_application::create_purchase::CreatePurchaseDecision;
use bookkeeping_postgres::create_purchase::{
    CreatePurchasePersistenceError, PurchaseCreationCrossContext,
};
use diesel::{ExpressionMethods, JoinOnDsl, OptionalExtension, QueryDsl, dsl::now};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

async fn validate_catalog_references(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    decision: &CreatePurchaseDecision,
) -> Result<(), CreatePurchasePersistenceError> {
    use public::accounts::dsl as accounts;
    use public::allocation_defaults::dsl as defaults;
    use public::allocation_rule_weights::dsl as rule_weights;
    use public::categories::dsl as categories;
    use public::households::dsl as households;
    use public::pools::dsl as pools;
    use public::product_concepts::dsl as concepts;
    use public::product_variants::dsl as variants;

    let default_version =
        i64::try_from(decision.allocation_rule_version.value()).map_err(|_| {
            CreatePurchasePersistenceError::NumericOutOfRange("allocation_rule_version")
        })?;
    let current_default_version = households::households
        .filter(households::id.eq(household_id))
        .select(households::default_allocation_version)
        .first::<i64>(conn)
        .await
        .optional()?
        .ok_or(CreatePurchasePersistenceError::HouseholdNotFound)?;
    if current_default_version != default_version {
        return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
    }
    let configured_defaults = defaults::allocation_defaults
        .filter(defaults::household_id.eq(household_id))
        .filter(defaults::version.eq(default_version))
        .select(defaults::pool_id)
        .load::<Uuid>(conn)
        .await?;
    if configured_defaults.is_empty() {
        return Err(CreatePurchasePersistenceError::MissingDefaultAllocation);
    }

    for payment in &decision.payments {
        let account_id = parse_uuid(&payment.account_id, "account_id")?;
        let found = accounts::accounts
            .filter(accounts::household_id.eq(household_id))
            .filter(accounts::id.eq(account_id))
            .select(accounts::id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some();
        if !found {
            return Err(CreatePurchasePersistenceError::ReferenceNotFound("account"));
        }
    }

    for item in &decision.items {
        if let Some(category_id) = item.category_id.as_deref() {
            let category_id = parse_uuid(category_id, "category_id")?;
            let found = categories::categories
                .filter(categories::household_id.eq(household_id))
                .filter(categories::id.eq(category_id))
                .select(categories::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !found {
                return Err(CreatePurchasePersistenceError::ReferenceNotFound(
                    "category",
                ));
            }
        }
        let mut concept_id = None;
        if let Some(variant_id) = item.product_variant_id.as_deref() {
            let variant_id = parse_uuid(variant_id, "product_variant_id")?;
            let found_concept_id = variants::product_variants
                .filter(variants::household_id.eq(household_id))
                .filter(variants::id.eq(variant_id))
                .filter(variants::archived.eq(false))
                .inner_join(concepts::product_concepts.on(concepts::id.eq(variants::concept_id)))
                .filter(concepts::household_id.eq(household_id))
                .filter(concepts::archived.eq(false))
                .select(variants::concept_id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .ok_or(CreatePurchasePersistenceError::ReferenceNotFound(
                    "product_variant",
                ))?;
            concept_id = Some(found_concept_id);
        }

        let first = item
            .allocations
            .as_slice()
            .first()
            .ok_or(CreatePurchasePersistenceError::InvalidDecision)?;
        if item.allocations.iter().any(|allocation| {
            allocation.rule_version != first.rule_version || allocation.rule_id != first.rule_id
        }) {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }
        for allocation in &item.allocations {
            let pool_id = parse_uuid(&allocation.pool_id, "pool_id")?;
            let found = pools::pools
                .filter(pools::household_id.eq(household_id))
                .filter(pools::id.eq(pool_id))
                .select(pools::id)
                .first::<Uuid>(conn)
                .await
                .optional()?
                .is_some();
            if !found {
                return Err(CreatePurchasePersistenceError::ReferenceNotFound("pool"));
            }
        }
        if item.allocation_override {
            if first.rule_id.is_some()
                || first.rule_version != decision.allocation_rule_version.value()
            {
                return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
            }
            continue;
        }

        let merchant_key = decision
            .merchant
            .split_whitespace()
            .collect::<Vec<_>>()
            .join(" ")
            .to_lowercase();
        let candidates = eligible_allocation_rule_candidates(
            conn,
            household_id,
            concept_id,
            item.category_id.as_deref(),
            item.product_variant_id.as_deref(),
            &merchant_key,
        )
        .await?;
        let selected_rule = bookkeeping_application::create_purchase::select_allocation_rule(
            &candidates,
            item.product_variant_id.as_deref(),
            concept_id.map(|id| id.to_string()).as_deref(),
            item.category_id.as_deref(),
            &merchant_key,
        );
        if selected_rule.map(|candidate| candidate.rule_id.as_str()) != first.rule_id.as_deref() {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }
        if first.rule_id.is_none() && first.rule_version != decision.allocation_rule_version.value()
        {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }

        let expected = if let Some(rule_id) = first.rule_id.as_deref() {
            let rule_id = parse_uuid(rule_id, "allocation_rule_id")?;
            let revision_matches = candidates.iter().any(|candidate| {
                candidate.rule_id == rule_id.to_string() && candidate.revision == first.rule_version
            });
            if !revision_matches {
                return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
            }
            rule_weights::allocation_rule_weights
                .filter(rule_weights::household_id.eq(household_id))
                .filter(rule_weights::rule_id.eq(rule_id))
                .select((rule_weights::pool_id, rule_weights::weight))
                .load::<(Uuid, i64)>(conn)
                .await?
        } else {
            let version = i64::try_from(first.rule_version)
                .map_err(|_| CreatePurchasePersistenceError::NumericOutOfRange("rule_version"))?;
            defaults::allocation_defaults
                .filter(defaults::household_id.eq(household_id))
                .filter(defaults::version.eq(version))
                .select((defaults::pool_id, defaults::weight))
                .load::<(Uuid, i64)>(conn)
                .await?
        };
        let mut expected = expected;
        let mut actual = item
            .allocations
            .iter()
            .map(|allocation| {
                Ok((
                    parse_uuid(&allocation.pool_id, "pool_id")?,
                    allocation.weight,
                ))
            })
            .collect::<Result<Vec<_>, CreatePurchasePersistenceError>>()?;
        expected.sort_unstable();
        actual.sort_unstable();
        if expected != actual {
            return Err(CreatePurchasePersistenceError::AllocationRuleMismatch);
        }
    }
    Ok(())
}

async fn eligible_allocation_rule_candidates(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    concept_id: Option<Uuid>,
    category_id: Option<&str>,
    variant_id: Option<&str>,
    merchant_key: &str,
) -> Result<
    Vec<bookkeeping_application::create_purchase::AllocationRuleCandidate>,
    CreatePurchasePersistenceError,
> {
    use public::allocation_rules::dsl as rules;

    let category_id = category_id
        .map(|id| parse_uuid(id, "category_id"))
        .transpose()?;
    let variant_id = variant_id
        .map(|id| parse_uuid(id, "product_variant_id"))
        .transpose()?;
    let mut query = rules::allocation_rules.into_boxed();
    let mut has_candidate_scope = false;
    if let Some(variant_id) = variant_id {
        query = query.or_filter(rules::product_variant_id.eq(Some(variant_id)));
        has_candidate_scope = true;
    }
    if let Some(concept_id) = concept_id {
        query = query.or_filter(rules::product_concept_id.eq(Some(concept_id)));
        has_candidate_scope = true;
    }
    if let Some(category_id) = category_id {
        query = query.or_filter(rules::category_id.eq(Some(category_id)));
        has_candidate_scope = true;
    }
    if !merchant_key.is_empty() {
        query = query.or_filter(rules::merchant_key.eq(Some(merchant_key.to_owned())));
        has_candidate_scope = true;
    }
    if !has_candidate_scope {
        return Ok(Vec::new());
    }
    query
        .filter(rules::household_id.eq(household_id))
        .select((
            rules::id,
            rules::revision,
            rules::product_variant_id,
            rules::product_concept_id,
            rules::category_id,
            rules::merchant_key,
        ))
        .load::<(
            Uuid,
            i64,
            Option<Uuid>,
            Option<Uuid>,
            Option<Uuid>,
            Option<String>,
        )>(conn)
        .await?
        .into_iter()
        .map(
            |(id, revision, variant_id, concept_id, category_id, merchant)| {
                Ok(
                    bookkeeping_application::create_purchase::AllocationRuleCandidate {
                        rule_id: id.to_string(),
                        revision: u64::try_from(revision)
                            .map_err(|_| CreatePurchasePersistenceError::AllocationRuleMismatch)?,
                        product_variant_id: variant_id.map(|id| id.to_string()),
                        product_concept_id: concept_id.map(|id| id.to_string()),
                        category_id: category_id.map(|id| id.to_string()),
                        merchant_key: merchant,
                    },
                )
            },
        )
        .collect::<Result<Vec<_>, CreatePurchasePersistenceError>>()
}

async fn resolve_source_confirmation(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    source_document_id: Option<&str>,
    source_reference: &str,
) -> Result<Option<Uuid>, CreatePurchasePersistenceError> {
    let Some(source_document_id) = source_document_id else {
        return Ok(None);
    };
    use public::source_confirmations::dsl as confirmations;
    use public::source_documents::dsl as documents;

    let source_document_id = parse_uuid(source_document_id, "source_document_id")?;
    let found = documents::source_documents
        .filter(documents::household_id.eq(household_id))
        .filter(documents::id.eq(source_document_id))
        .for_update()
        .select(documents::id)
        .first::<Uuid>(conn)
        .await
        .optional()?
        .is_some();
    if !found {
        return Err(CreatePurchasePersistenceError::ReferenceNotFound(
            "source_document",
        ));
    }
    let latest = confirmations::source_confirmations
        .filter(confirmations::household_id.eq(household_id))
        .filter(confirmations::source_document_id.eq(source_document_id))
        .filter(confirmations::source_reference.eq(source_reference))
        .order_by(confirmations::changed_at.desc())
        .select((
            confirmations::entity_kind,
            confirmations::entity_id,
            confirmations::status,
        ))
        .first::<(String, Uuid, String)>(conn)
        .await
        .optional()?;
    match latest {
        Some((kind, entity_id, status)) if status == "confirmed" && kind == "purchase" => {
            Ok(Some(entity_id))
        }
        Some((_, _, status)) if status == "confirmed" => {
            Err(CreatePurchasePersistenceError::SourceAlreadyConfirmedAsCorrection)
        }
        _ => Ok(None),
    }
}

async fn persist_source_evidence(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    source_document_id: Uuid,
    source_reference: &str,
    purchase_id: Uuid,
    actor_id: Uuid,
    decision: &CreatePurchaseDecision,
) -> Result<(), CreatePurchasePersistenceError> {
    use public::extraction_jobs::dsl as jobs;

    let mut observation = serde_json::json!({});
    let mut receipt_items = Vec::new();
    let mut region_transform = serde_json::Value::Null;
    let mut transform_page = 0_i64;
    let mut receipt_total = None;
    let mut confirmed_total = None;
    let mut extraction_job_id = None;

    if source_reference.is_empty() {
        let latest = jobs::extraction_jobs
            .filter(jobs::household_id.eq(household_id))
            .filter(jobs::source_document_id.eq(source_document_id))
            .filter(jobs::status.eq("succeeded"))
            .order_by(jobs::version.desc())
            .select((jobs::id, jobs::result))
            .first::<(Uuid, Option<serde_json::Value>)>(conn)
            .await
            .optional()?;
        if let Some((job_id, Some(result))) = latest {
            let receipt = result
                .get("receipt")
                .cloned()
                .unwrap_or(serde_json::Value::Null);
            receipt_items = receipt
                .get("items")
                .and_then(serde_json::Value::as_array)
                .cloned()
                .unwrap_or_default();
            let candidate_id = decision.receipt_candidate_id.as_deref().unwrap_or("");
            let override_reason = decision
                .manual_amount_override_reason
                .as_deref()
                .unwrap_or_default()
                .trim();
            if receipt_items.is_empty() && (!candidate_id.is_empty() || !override_reason.is_empty())
            {
                return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
            }
            let amounts = decision
                .items
                .iter()
                .map(|item| item.amount_yen)
                .collect::<Vec<_>>();
            if !candidate_id.is_empty() {
                let candidates = result
                    .get("candidates")
                    .and_then(serde_json::Value::as_array)
                    .cloned()
                    .unwrap_or_default();
                let candidate = candidates
                    .iter()
                    .find(|candidate| {
                        candidate.get("id").and_then(serde_json::Value::as_str)
                            == Some(candidate_id)
                    })
                    .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)?;
                let candidate_amounts = candidate
                    .get("amounts")
                    .and_then(serde_json::Value::as_array)
                    .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)?
                    .iter()
                    .map(|amount| {
                        amount
                            .as_i64()
                            .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                if override_reason.is_empty() && candidate_amounts != amounts {
                    return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
                }
            } else if receipt_items.len() == amounts.len() && override_reason.is_empty() {
                let extracted_amounts = receipt_items
                    .iter()
                    .map(|item| {
                        item.get("grossAmount")
                            .and_then(serde_json::Value::as_i64)
                            .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)
                    })
                    .collect::<Result<Vec<_>, _>>()?;
                if extracted_amounts != amounts {
                    return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
                }
            }
            let total = receipt
                .get("total")
                .and_then(serde_json::Value::as_i64)
                .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)?;
            let calculated_total = amounts.iter().try_fold(0_i64, |sum, amount| {
                if *amount < 0 || *amount > 1_000_000_000_000_i64.saturating_sub(sum) {
                    return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
                }
                sum.checked_add(*amount)
                    .ok_or(CreatePurchasePersistenceError::ReceiptConfirmationInvalid)
            })?;
            if override_reason.is_empty() && total != calculated_total {
                return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
            }
            confirmed_total = Some(calculated_total);
            extraction_job_id = Some(job_id.to_string());
            transform_page = result
                .get("transformPage")
                .and_then(serde_json::Value::as_i64)
                .unwrap_or(0);
            region_transform = result
                .get("transform")
                .cloned()
                .unwrap_or(serde_json::Value::Null);
            receipt_total = Some(total);
            let mut values = serde_json::json!({
                "jobId": job_id.to_string(),
                "receipt": receipt,
                "transform": region_transform,
                "transformPage": transform_page,
                    "confirmedTotal": calculated_total,
                "confirmedItems": decision.items.iter().map(confirmed_item_json).collect::<Vec<_>>(),
            });
            if !candidate_id.is_empty() {
                values["receiptCandidateId"] = serde_json::Value::String(candidate_id.to_owned());
            }
            if !override_reason.is_empty() {
                values["manualAmountOverrideReason"] =
                    serde_json::Value::String(override_reason.to_owned());
            }
            observation = values;
        } else if decision.receipt_candidate_id.is_some()
            || decision
                .manual_amount_override_reason
                .as_deref()
                .is_some_and(|reason| !reason.trim().is_empty())
        {
            return Err(CreatePurchasePersistenceError::ReceiptConfirmationInvalid);
        }
    }

    let item_evidence_matches = source_reference.is_empty()
        && !receipt_items.is_empty()
        && receipt_items.len() == decision.items.len()
        && receipt_items
            .iter()
            .zip(&decision.items)
            .all(|(receipt_item, item)| {
                let extracted_name = receipt_item
                    .get("name")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .trim();
                let printed_name = receipt_item
                    .get("printedName")
                    .and_then(serde_json::Value::as_str)
                    .unwrap_or("")
                    .trim();
                let name = item.name.trim();
                name == extracted_name || name == printed_name
            });

    if item_evidence_matches {
        for (index, (receipt_item, item)) in receipt_items.iter().zip(&decision.items).enumerate() {
            let mut value = serde_json::json!({
                "jobId": extraction_job_id,
                "itemIndex": index,
                "item": receipt_item,
                "confirmedItem": confirmed_item_json(item),
                "transform": region_transform,
                "transformPage": transform_page,
                "confirmedTotal": confirmed_total,
            });
            if let Some(total) = receipt_total {
                value["receiptTotal"] = serde_json::json!(total);
            }
            if let Some(candidate_id) = decision.receipt_candidate_id.as_deref() {
                value["receiptCandidateId"] = serde_json::Value::String(candidate_id.to_owned());
            }
            if let Some(reason) = decision
                .manual_amount_override_reason
                .as_deref()
                .filter(|reason| !reason.trim().is_empty())
            {
                value["manualAmountOverrideReason"] =
                    serde_json::Value::String(reason.trim().to_owned());
            }
            let item_id = parse_uuid(&item.item_id, "item_id")?;
            horse_postgres_common::insert_evidence_link(
                conn,
                evidence_application::EvidenceLink {
                    created_at: chrono::Utc::now(),
                    household_id,
                    source_document_id,
                    entity_kind: "purchase",
                    entity_id: purchase_id,
                    item_id: Some(item_id),
                    purchase_id: Some(purchase_id),
                    correction_id: None,
                    transfer_id: None,
                    evidence_region: receipt_item.get("region").cloned(),
                    observation: value,
                    actor_id,
                    source_reference: source_reference.to_owned(),
                    ignore_duplicate: true,
                },
            )
            .await?;
        }
    } else {
        if let Some(candidate_id) = decision.receipt_candidate_id.as_deref() {
            observation["receiptCandidateId"] = serde_json::Value::String(candidate_id.to_owned());
        }
        if let Some(reason) = decision
            .manual_amount_override_reason
            .as_deref()
            .filter(|reason| !reason.trim().is_empty())
        {
            observation["manualAmountOverrideReason"] =
                serde_json::Value::String(reason.trim().to_owned());
        }
        horse_postgres_common::insert_evidence_link(
            conn,
            evidence_application::EvidenceLink {
                created_at: chrono::Utc::now(),
                household_id,
                source_document_id,
                entity_kind: "purchase",
                entity_id: purchase_id,
                item_id: None,
                purchase_id: Some(purchase_id),
                correction_id: None,
                transfer_id: None,
                evidence_region: None,
                observation,
                actor_id,
                source_reference: source_reference.to_owned(),
                ignore_duplicate: true,
            },
        )
        .await?;
    }
    Ok(())
}

fn confirmed_item_json(
    item: &bookkeeping_application::create_purchase::DecidedPurchaseItem,
) -> serde_json::Value {
    serde_json::json!({
        "purchaseItemId": item.item_id,
        "name": item.name,
        "amountYen": item.amount_yen,
        "categoryId": item.category_id,
        "productVariantId": item.product_variant_id,
        "allocations": item.allocations.iter().map(|allocation| serde_json::json!({
            "poolId": allocation.pool_id,
            "weight": allocation.weight,
        })).collect::<Vec<_>>(),
        "quantity": item.quantity.as_ref().map(|measure| serde_json::json!({
            "numerator": measure.numerator(),
            "denominator": measure.denominator(),
            "unit": quantity_unit(measure.unit()),
        })),
        "printedUnitPrice": item.printed_unit_price.as_ref().map(|price| serde_json::json!({
            "amountYen": price.amount_yen,
            "basis": {
                "numerator": price.basis.numerator(),
                "denominator": price.basis.denominator(),
                "unit": quantity_unit(price.basis.unit()),
            },
        })),
    })
}

#[derive(Default)]
pub struct PurchaseCreationContext;

impl PurchaseCreationCrossContext for PurchaseCreationContext {
    async fn require_active_member(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        actor_id: Uuid,
    ) -> Result<bool, CreatePurchasePersistenceError> {
        use public::household_members::dsl as members;
        Ok(members::household_members
            .filter(members::household_id.eq(household_id))
            .filter(members::member_id.eq(actor_id))
            .filter(members::left_at.is_null())
            .select(members::member_id)
            .first::<Uuid>(conn)
            .await
            .optional()?
            .is_some())
    }
    async fn current_default_allocation_version(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
    ) -> Result<i64, CreatePurchasePersistenceError> {
        use public::households::dsl as households;
        households::households
            .filter(households::id.eq(household_id))
            .for_share()
            .select(households::default_allocation_version)
            .first::<i64>(conn)
            .await
            .optional()?
            .ok_or(CreatePurchasePersistenceError::HouseholdNotFound)
    }
    async fn validate_catalog_references(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        decision: &CreatePurchaseDecision,
    ) -> Result<(), CreatePurchasePersistenceError> {
        validate_catalog_references(conn, household_id, decision).await
    }
    async fn resolve_source_confirmation(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        source_document_id: Option<&str>,
        source_reference: &str,
    ) -> Result<Option<Uuid>, CreatePurchasePersistenceError> {
        resolve_source_confirmation(conn, household_id, source_document_id, source_reference).await
    }
    async fn persist_source_evidence(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        source_document_id: Uuid,
        source_reference: &str,
        purchase_id: Uuid,
        actor_id: Uuid,
        decision: &CreatePurchaseDecision,
    ) -> Result<(), CreatePurchasePersistenceError> {
        persist_source_evidence(
            conn,
            household_id,
            source_document_id,
            source_reference,
            purchase_id,
            actor_id,
            decision,
        )
        .await
    }
    async fn persist_source_confirmation(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        actor_id: Uuid,
        idempotency_key: &str,
        source_document_id: Uuid,
        source_reference: &str,
        purchase_id: Uuid,
    ) -> Result<(), CreatePurchasePersistenceError> {
        diesel::insert_into(public::source_confirmations::table)
            .values((
                public::source_confirmations::id.eq(Uuid::new_v4()),
                public::source_confirmations::household_id.eq(household_id),
                public::source_confirmations::source_document_id.eq(source_document_id),
                public::source_confirmations::observation_id.eq(None::<Uuid>),
                public::source_confirmations::source_reference.eq(source_reference),
                public::source_confirmations::entity_kind.eq("purchase"),
                public::source_confirmations::entity_id.eq(purchase_id),
                public::source_confirmations::status.eq("confirmed"),
                public::source_confirmations::idempotency_key.eq(idempotency_key),
                public::source_confirmations::changed_by.eq(actor_id),
                public::source_confirmations::changed_at.eq(now),
            ))
            .execute(conn)
            .await?;
        Ok(())
    }
    async fn publish_purchase_created(
        &mut self,
        conn: &mut diesel_async::AsyncPgConnection,
        household_id: Uuid,
        actor_id: Uuid,
        purchase_id: Uuid,
        change: horse_household_change::UiChangeDescriptor,
    ) -> Result<(), CreatePurchasePersistenceError> {
        horse_postgres_common::insert_ui_change(
            conn,
            household_id,
            purchase_id,
            1,
            actor_id,
            chrono::Utc::now(),
            change,
        )
        .await?;
        Ok(())
    }
}

fn quantity_unit(value: bookkeeping_application::create_purchase::QuantityUnit) -> &'static str {
    use bookkeeping_application::create_purchase::QuantityUnit as U;
    match value {
        U::Piece => "個",
        U::Bottle => "本",
        U::Sheet => "枚",
        U::Bag => "袋",
        U::Box => "箱",
        U::Gram => "g",
        U::Kilogram => "kg",
        U::Milliliter => "ml",
        U::Liter => "L",
    }
}

fn parse_uuid(value: &str, field: &'static str) -> Result<Uuid, CreatePurchasePersistenceError> {
    Uuid::parse_str(value).map_err(|_| CreatePurchasePersistenceError::InvalidUuid(field))
}
