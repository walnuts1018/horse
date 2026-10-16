use async_trait::async_trait;
use uuid::Uuid;

use crate::purchase_command_result;
use cross_context_application::purchase_correction_query as application;
use horse_postgres_common::PgPool;

pub struct PostgresPurchaseCorrectionQuery {
    pool: PgPool,
}

impl PostgresPurchaseCorrectionQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl application::PurchaseCorrectionQuery for PostgresPurchaseCorrectionQuery {
    async fn load(
        &self,
        actor_id: Uuid,
        purchase_id: Uuid,
        first: Option<i32>,
        after: Option<String>,
    ) -> Result<application::PurchaseCorrectionConnection, application::PurchaseCorrectionQueryError>
    {
        let household_id =
            purchase_command_result::load_purchase_household_id(&self.pool, purchase_id)
                .await
                .map_err(|error| {
                    use purchase_command_result::PurchaseCommandResultError as E;
                    match error {
                        E::PurchaseNotFound => {
                            application::PurchaseCorrectionQueryError::ResourceUnavailable
                        }
                        E::Pool(error) => {
                            application::PurchaseCorrectionQueryError::Infrastructure(error)
                        }
                        E::Database(error) => {
                            application::PurchaseCorrectionQueryError::Infrastructure(
                                error.to_string(),
                            )
                        }
                        E::NumericOutOfRange | E::InvalidPurchaseStatus => {
                            application::PurchaseCorrectionQueryError::Infrastructure(format!(
                                "{error:?}"
                            ))
                        }
                    }
                })?
                .ok_or(application::PurchaseCorrectionQueryError::ResourceUnavailable)?;

        let is_member = household_postgres::household_access::is_active_member(
            &self.pool,
            household_id,
            actor_id,
        )
        .await
        .map_err(|error| {
            use household_postgres::household_access::HouseholdAccessError as E;
            match error {
                E::Pool(message) => {
                    application::PurchaseCorrectionQueryError::Infrastructure(message)
                }
                E::Database(error) => {
                    application::PurchaseCorrectionQueryError::Infrastructure(error.to_string())
                }
            }
        })?;
        if !is_member {
            return Err(application::PurchaseCorrectionQueryError::ResourceUnavailable);
        }

        let projection = bookkeeping_postgres::purchase_corrections::load_purchase_corrections(
            &self.pool,
            household_id,
            purchase_id,
            first,
            after.as_deref(),
        )
        .await
        .map_err(|error| {
            use bookkeeping_postgres::purchase_corrections::PurchaseCorrectionError as E;
            match error {
                E::PurchaseNotFound => application::PurchaseCorrectionQueryError::PurchaseNotFound,
                E::CursorNotFound => application::PurchaseCorrectionQueryError::CursorNotFound,
                E::InvalidPageSize => application::PurchaseCorrectionQueryError::InvalidPageSize,
                E::InvalidCursor => application::PurchaseCorrectionQueryError::InvalidCursor,
                E::Pool(error) => application::PurchaseCorrectionQueryError::Infrastructure(error),
                E::Database(error) => {
                    application::PurchaseCorrectionQueryError::Infrastructure(error.to_string())
                }
                E::NumericOutOfRange => application::PurchaseCorrectionQueryError::Infrastructure(
                    "numeric out of range".to_owned(),
                ),
            }
        })?;

        let edges = projection
            .edges
            .into_iter()
            .map(|correction| {
                let cursor = bookkeeping_postgres::purchase_corrections::encode_cursor(&correction);
                application::PurchaseCorrectionEdge {
                    cursor,
                    node: application::PurchaseCorrection {
                        id: correction.id,
                        item_id: correction.item_id,
                        amount_delta_yen: correction.amount_delta_yen,
                        reason: correction.reason,
                        occurred_at: correction.occurred_at,
                        settlement_month: correction.settlement_month,
                        payment_allocations: correction
                            .payment_allocations
                            .into_iter()
                            .map(|payment| application::PurchaseCorrectionPayment {
                                account_id: payment.account_id,
                                account_name: payment.account_name,
                                account_kind: payment.account_kind,
                                account_card_last4: payment.account_card_last4,
                                account_pool_id: payment.account_pool_id,
                                amount_yen: payment.amount_yen,
                            })
                            .collect(),
                    },
                }
            })
            .collect();
        Ok(application::PurchaseCorrectionConnection {
            edges,
            total_count: projection.total_count,
            has_next_page: projection.has_next_page,
            has_previous_page: projection.has_previous_page,
        })
    }
}
