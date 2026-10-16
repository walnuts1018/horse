use diesel::{BoolExpressionMethods, ExpressionMethods, JoinOnDsl, QueryDsl};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use bookkeeping_application::purchase_node_batch_query::{
    PurchaseCorrectionPaymentProjection, PurchaseCorrectionProjection,
    PurchaseItemParentProjection, PurchaseNodeBatchProjection,
    PurchaseNodeBatchQuery as PurchaseNodeBatchQueryPort, PurchaseNodeBatchQueryError,
};

use crate::{PgPool, schema::public};

type PurchaseCorrectionRow = (
    Uuid,
    Uuid,
    Uuid,
    Option<Uuid>,
    i64,
    String,
    chrono::NaiveDate,
    chrono::DateTime<chrono::Utc>,
);

type CorrectionPaymentRow = (
    Uuid,
    Uuid,
    String,
    String,
    Option<String>,
    Option<Uuid>,
    i64,
);

pub struct PostgresPurchaseNodeBatchQuery {
    pool: PgPool,
}

impl PostgresPurchaseNodeBatchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl PurchaseNodeBatchQueryPort for PostgresPurchaseNodeBatchQuery {
    fn query<'a>(
        &'a self,
        household_ids: &'a [Uuid],
        purchase_ids: &'a [Uuid],
        item_ids: &'a [Uuid],
        correction_ids: &'a [Uuid],
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<
                    Output = Result<PurchaseNodeBatchProjection, PurchaseNodeBatchQueryError>,
                > + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            if household_ids.is_empty()
                || (purchase_ids.is_empty() && item_ids.is_empty() && correction_ids.is_empty())
            {
                return Ok(PurchaseNodeBatchProjection::default());
            }

            let mut conn =
                self.pool.get().await.map_err(|error| {
                    PurchaseNodeBatchQueryError::Infrastructure(error.to_string())
                })?;

            let item_parents = if item_ids.is_empty() {
                Vec::new()
            } else {
                public::purchase_items::table
                    .filter(public::purchase_items::household_id.eq_any(household_ids))
                    .filter(public::purchase_items::id.eq_any(item_ids))
                    .select((
                        public::purchase_items::household_id,
                        public::purchase_items::purchase_id,
                        public::purchase_items::id,
                    ))
                    .load::<(Uuid, Uuid, Uuid)>(&mut *conn)
                    .await
                    .map_err(|error| {
                        PurchaseNodeBatchQueryError::Infrastructure(format!("{error:?}"))
                    })?
                    .into_iter()
                    .map(
                        |(household_id, purchase_id, item_id)| PurchaseItemParentProjection {
                            household_id,
                            purchase_id,
                            item_id,
                        },
                    )
                    .collect()
            };

            let corrections = if correction_ids.is_empty() {
                Vec::new()
            } else {
                public::purchase_corrections::table
                    .inner_join(
                        public::journals::table.on(public::journals::id
                            .eq(public::purchase_corrections::journal_id)
                            .and(
                                public::journals::household_id
                                    .eq(public::purchase_corrections::household_id),
                            )),
                    )
                    .filter(public::purchase_corrections::household_id.eq_any(household_ids))
                    .filter(public::purchase_corrections::id.eq_any(correction_ids))
                    .select((
                        public::purchase_corrections::household_id,
                        public::purchase_corrections::purchase_id,
                        public::purchase_corrections::id,
                        public::purchase_corrections::item_id,
                        public::purchase_corrections::amount_delta_yen,
                        public::purchase_corrections::reason,
                        public::purchase_corrections::effective_period_start,
                        public::journals::occurred_at,
                    ))
                    .load::<PurchaseCorrectionRow>(&mut *conn)
                    .await
                    .map_err(|error| {
                        PurchaseNodeBatchQueryError::Infrastructure(format!("{error:?}"))
                    })?
                    .into_iter()
                    .map(
                        |(
                            household_id,
                            purchase_id,
                            id,
                            item_id,
                            amount_delta_yen,
                            reason,
                            settlement_month,
                            occurred_at,
                        )| PurchaseCorrectionProjection {
                            household_id,
                            purchase_id,
                            id,
                            item_id,
                            amount_delta_yen,
                            reason,
                            settlement_month,
                            occurred_at,
                        },
                    )
                    .collect()
            };

            let correction_ids = corrections.iter().map(|row| row.id).collect::<Vec<_>>();
            let correction_payments = if correction_ids.is_empty() {
                Vec::new()
            } else {
                public::purchase_correction_payments::table
                    .inner_join(
                        public::accounts::table.on(public::accounts::id
                            .eq(public::purchase_correction_payments::account_id)
                            .and(
                                public::accounts::household_id
                                    .eq(public::purchase_correction_payments::household_id),
                            )),
                    )
                    .filter(
                        public::purchase_correction_payments::household_id.eq_any(household_ids),
                    )
                    .filter(
                        public::purchase_correction_payments::correction_id.eq_any(correction_ids),
                    )
                    .order_by((
                        public::purchase_correction_payments::correction_id,
                        public::purchase_correction_payments::line_no,
                    ))
                    .select((
                        public::purchase_correction_payments::correction_id,
                        public::accounts::id,
                        public::accounts::name,
                        public::accounts::kind,
                        public::accounts::card_last4,
                        public::accounts::pool_id,
                        public::purchase_correction_payments::amount_yen,
                    ))
                    .load::<CorrectionPaymentRow>(&mut *conn)
                    .await
                    .map_err(|error| {
                        PurchaseNodeBatchQueryError::Infrastructure(format!("{error:?}"))
                    })?
                    .into_iter()
                    .map(
                        |(
                            correction_id,
                            account_id,
                            account_name,
                            account_kind,
                            card_last4,
                            pool_id,
                            amount_yen,
                        )| PurchaseCorrectionPaymentProjection {
                            correction_id,
                            account_id,
                            account_name,
                            account_kind,
                            card_last4,
                            pool_id,
                            amount_yen,
                        },
                    )
                    .collect()
            };

            Ok(PurchaseNodeBatchProjection {
                item_parents,
                corrections,
                correction_payments,
            })
        })
    }
}
