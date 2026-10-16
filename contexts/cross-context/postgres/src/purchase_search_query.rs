use chrono::{DateTime, Utc};
use diesel::pg::Pg;
use diesel::{
    BoolExpressionMethods, ExpressionMethods, JoinOnDsl, NullableExpressionMethods,
    OptionalExtension, PgTextExpressionMethods, QueryDsl,
    dsl::{exists, max},
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, schema::public};

use bookkeeping_application::purchase_search_query::{
    PurchaseItemAllocationProjection, PurchaseItemProjection, PurchasePaymentProjection,
    PurchaseProjection, PurchaseSearchCursor as ApplicationCursor,
    PurchaseSearchFilter as ApplicationFilter, PurchaseSearchPage as ApplicationPage,
    PurchaseSearchQuery as ApplicationQuery, PurchaseSearchQueryError as ApplicationError,
    PurchaseSearchRow as ApplicationRow,
};

use crate::purchase_command_result::{PurchaseCommandResult, PurchaseCommandResultError};

#[derive(Clone, Debug)]
pub struct PurchaseSearchFilter {
    pub household_id: Uuid,
    pub from: Option<DateTime<Utc>>,
    pub to: Option<DateTime<Utc>>,
    pub origin: Option<String>,
    pub has_evidence: Option<bool>,
    pub search: Option<String>,
    pub category_id: Option<Uuid>,
    pub uncategorized: bool,
    pub tag_id: Option<Uuid>,
    pub payment_account_id: Option<Uuid>,
    pub pool_id: Option<Uuid>,
    pub minimum_amount: Option<i64>,
    pub maximum_amount: Option<i64>,
    pub amount_target: PurchaseAmountTarget,
}

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum PurchaseAmountTarget {
    Purchase,
    Item,
}

#[derive(Clone, Debug)]
pub struct PurchaseSearchCursor {
    pub id: Uuid,
    pub occurred_at: Option<DateTime<Utc>>,
}

#[derive(Clone, Debug)]
pub struct PurchaseSearchRow {
    pub id: Uuid,
    pub occurred_at: DateTime<Utc>,
}

#[derive(Clone, Debug)]
pub struct PurchaseSearchPage {
    pub rows: Vec<PurchaseSearchRow>,
    pub total_count: i64,
    pub sync_cursor: i64,
    pub has_extra: bool,
    pub backward: bool,
}

#[derive(Debug)]
pub enum PurchaseSearchQueryError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    InvalidPagination,
    InvalidCursor,
    CursorNotFound,
    PurchaseNotFound,
    InvalidFilter,
}

pub struct PostgresPurchaseSearchQuery {
    pool: PgPool,
}

impl PostgresPurchaseSearchQuery {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

impl ApplicationQuery for PostgresPurchaseSearchQuery {
    fn query<'a>(
        &'a self,
        actor_id: Uuid,
        filter: ApplicationFilter,
        after: Option<ApplicationCursor>,
        before: Option<ApplicationCursor>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<ApplicationPage, ApplicationError>> + Send + 'a,
        >,
    > {
        Box::pin(async move {
            let household_id = filter.household_id;
            let page = purchases(
                &self.pool,
                actor_id,
                PurchaseSearchFilter {
                    household_id,
                    from: filter.from,
                    to: filter.to,
                    origin: filter.origin,
                    has_evidence: filter.has_evidence,
                    search: filter.search,
                    category_id: filter.category_id,
                    uncategorized: filter.uncategorized,
                    tag_id: filter.tag_id,
                    payment_account_id: filter.payment_account_id,
                    pool_id: filter.pool_id,
                    minimum_amount: filter.minimum_amount,
                    maximum_amount: filter.maximum_amount,
                    amount_target: match filter.amount_target {
                        bookkeeping_application::purchase_search_query::PurchaseAmountTarget::Purchase => PurchaseAmountTarget::Purchase,
                        bookkeeping_application::purchase_search_query::PurchaseAmountTarget::Item => PurchaseAmountTarget::Item,
                    },
                },
                after.map(|cursor| PurchaseSearchCursor {
                    id: cursor.id,
                    occurred_at: cursor.occurred_at,
                }),
                before.map(|cursor| PurchaseSearchCursor {
                    id: cursor.id,
                    occurred_at: cursor.occurred_at,
                }),
                first,
                last,
            )
            .await
            .map_err(application_error)?;
            let mut rows = Vec::with_capacity(page.rows.len());
            for row in page.rows {
                let result = crate::purchase_command_result::load_purchase_command_result(
                    &self.pool,
                    household_id,
                    row.id,
                )
                .await
                .map_err(|error| ApplicationError::Infrastructure(format!("{error:?}")))?;
                rows.push(ApplicationRow {
                    id: row.id,
                    occurred_at: row.occurred_at,
                    purchase: to_projection(result),
                });
            }
            Ok(ApplicationPage {
                rows,
                total_count: page.total_count,
                sync_cursor: page.sync_cursor,
                has_extra: page.has_extra,
                backward: page.backward,
            })
        })
    }

    fn purchase<'a>(
        &'a self,
        actor_id: Uuid,
        household_id: Uuid,
        purchase_id: Uuid,
    ) -> std::pin::Pin<
        Box<
            dyn std::future::Future<Output = Result<PurchaseProjection, ApplicationError>>
                + Send
                + 'a,
        >,
    > {
        Box::pin(async move {
            let active =
                crate::household_access::is_active_member(&self.pool, household_id, actor_id)
                    .await
                    .map_err(|error| ApplicationError::Infrastructure(format!("{error:?}")))?;
            if !active {
                return Err(ApplicationError::HouseholdNotFound);
            }
            crate::purchase_command_result::load_purchase_command_result(
                &self.pool,
                household_id,
                purchase_id,
            )
            .await
            .map(to_projection)
            .map_err(|error| match error {
                PurchaseCommandResultError::PurchaseNotFound => ApplicationError::PurchaseNotFound,
                error => ApplicationError::Infrastructure(format!("{error:?}")),
            })
        })
    }
}

fn to_projection(result: PurchaseCommandResult) -> PurchaseProjection {
    PurchaseProjection {
        id: result.id,
        household_id: result.household_id,
        occurred_at: result.occurred_at,
        date_precision: result.date_precision,
        merchant: result.merchant,
        origin: result.origin,
        status: result.status,
        revision: result.revision,
        created_at: result.created_at,
        updated_at: result.updated_at,
        has_evidence: result.has_evidence,
        source_kinds: result.source_kinds,
        total_yen: result.total_yen,
        correction_amount_yen: result.correction_amount_yen,
        correction_count: result.correction_count,
        remaining_refundable_amount_yen: result.remaining_refundable_amount_yen,
        items: result
            .items
            .into_iter()
            .map(|item| PurchaseItemProjection {
                id: item.id,
                name: item.name,
                amount_yen: item.amount_yen,
                category_id: item.category_id,
                product_variant_id: item.product_variant_id,
                revision: item.revision,
                quantity_numerator: item.quantity_numerator,
                quantity_denominator: item.quantity_denominator,
                quantity_unit: item.quantity_unit,
                printed_unit_price_yen: item.printed_unit_price_yen,
                printed_unit_price_quantity_numerator: item.printed_unit_price_quantity_numerator,
                printed_unit_price_quantity_denominator: item
                    .printed_unit_price_quantity_denominator,
                printed_unit_price_unit: item.printed_unit_price_unit,
                allocations: item
                    .allocations
                    .into_iter()
                    .map(|allocation| PurchaseItemAllocationProjection {
                        pool_id: allocation.pool_id,
                        pool_name: allocation.pool_name,
                        weight: allocation.weight,
                        amount_yen: allocation.amount_yen,
                    })
                    .collect(),
                tags: item.tags,
                remaining_refundable_amount_yen: item.remaining_refundable_amount_yen,
            })
            .collect(),
        payments: result
            .payments
            .into_iter()
            .map(|payment| PurchasePaymentProjection {
                account_id: payment.account_id,
                account_name: payment.account_name,
                account_kind: payment.account_kind,
                account_card_last4: payment.account_card_last4,
                account_pool_id: payment.account_pool_id,
                amount_yen: payment.amount_yen,
            })
            .collect(),
    }
}

fn application_error(error: PurchaseSearchQueryError) -> ApplicationError {
    match error {
        PurchaseSearchQueryError::HouseholdNotFound => ApplicationError::HouseholdNotFound,
        PurchaseSearchQueryError::InvalidPagination => ApplicationError::InvalidPagination,
        PurchaseSearchQueryError::InvalidCursor => ApplicationError::InvalidCursor,
        PurchaseSearchQueryError::CursorNotFound => ApplicationError::CursorNotFound,
        PurchaseSearchQueryError::PurchaseNotFound => ApplicationError::PurchaseNotFound,
        PurchaseSearchQueryError::InvalidFilter => ApplicationError::InvalidFilter,
        PurchaseSearchQueryError::Pool(error) => ApplicationError::Infrastructure(error),
        PurchaseSearchQueryError::Database(error) => {
            ApplicationError::Infrastructure(error.to_string())
        }
    }
}

impl From<DieselError> for PurchaseSearchQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub async fn purchases(
    pool: &PgPool,
    actor_id: Uuid,
    filter: PurchaseSearchFilter,
    after: Option<PurchaseSearchCursor>,
    before: Option<PurchaseSearchCursor>,
    first: Option<i32>,
    last: Option<i32>,
) -> Result<PurchaseSearchPage, PurchaseSearchQueryError> {
    let (page_size, backward) = page_size(first, last)?;
    if filter.category_id.is_some() && filter.uncategorized {
        return Err(PurchaseSearchQueryError::InvalidFilter);
    }
    if filter
        .minimum_amount
        .zip(filter.maximum_amount)
        .is_some_and(|(min, max)| min > max)
    {
        return Err(PurchaseSearchQueryError::InvalidFilter);
    }
    let mut conn = pool
        .get()
        .await
        .map_err(|error| PurchaseSearchQueryError::Pool(error.to_string()))?;
    require_member(&mut conn, actor_id, filter.household_id).await?;

    use public::{
        evidence_links, household_events, purchase_item_tags, purchase_items, purchase_payments,
        purchases, source_confirmations,
    };

    let mut query = purchases::table
        .filter(purchases::household_id.eq(filter.household_id))
        .into_boxed::<Pg>();
    if let Some(origin) = &filter.origin {
        query = query.filter(purchases::origin.eq(origin));
    }
    if let Some(from) = filter.from {
        query = query.filter(purchases::occurred_at.ge(from));
    }
    if let Some(to) = filter.to {
        query = query.filter(purchases::occurred_at.lt(to));
    }
    if let Some(include_evidence) = filter.has_evidence {
        let evidence_exists = exists(
            evidence_links::table.filter(evidence_links::purchase_id.eq(purchases::id.nullable())),
        )
        .or(exists(source_confirmations::table.filter(
            source_confirmations::purchase_id.eq(purchases::id.nullable()),
        )));
        query = if include_evidence {
            query.filter(evidence_exists)
        } else {
            query.filter(diesel::dsl::not(evidence_exists))
        };
    }
    if let Some(account_id) = filter.payment_account_id {
        query = query.filter(exists(
            purchase_payments::table
                .filter(purchase_payments::purchase_id.eq(purchases::id))
                .filter(purchase_payments::account_id.eq(account_id)),
        ));
    }

    let search = filter
        .search
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(escape_like_pattern)
        .map(|value| format!("%{value}%"));
    let item_target = filter.amount_target == PurchaseAmountTarget::Item;
    let has_item_constraints = filter.category_id.is_some()
        || filter.uncategorized
        || filter.tag_id.is_some()
        || filter.pool_id.is_some()
        || (item_target && (filter.minimum_amount.is_some() || filter.maximum_amount.is_some()))
        || (item_target && search.is_some());
    if has_item_constraints {
        let mut items = purchase_items::table
            .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
            .filter(purchases::household_id.eq(filter.household_id))
            .into_boxed::<Pg>();
        if item_target {
            if let Some(minimum) = filter.minimum_amount {
                items = items.filter(purchase_items::amount_yen.ge(minimum));
            }
            if let Some(maximum) = filter.maximum_amount {
                items = items.filter(purchase_items::amount_yen.le(maximum));
            }
        }
        if let Some(category_id) = filter.category_id {
            items = items.filter(purchase_items::category_id.eq(Some(category_id)));
        } else if filter.uncategorized {
            items = items.filter(purchase_items::category_id.is_null());
        }
        if let Some(tag_id) = filter.tag_id {
            items = items.filter(exists(
                purchase_item_tags::table
                    .filter(purchase_item_tags::item_id.eq(purchase_items::id))
                    .filter(purchase_item_tags::tag_id.eq(tag_id)),
            ));
        }
        if let Some(pool_id) = filter.pool_id {
            items = items.filter(exists(
                public::purchase_allocations::table
                    .filter(public::purchase_allocations::item_id.eq(purchase_items::id))
                    .filter(public::purchase_allocations::purchase_id.eq(purchases::id))
                    .filter(public::purchase_allocations::pool_id.eq(pool_id)),
            ));
        }
        if item_target && let Some(pattern) = search.clone() {
            items = items.filter(
                purchases::merchant
                    .ilike(pattern.clone())
                    .or(purchase_items::name.ilike(pattern.clone())),
            );
        }
        let purchase_ids = items
            .select(purchase_items::purchase_id)
            .distinct()
            .load::<Uuid>(&mut conn)
            .await?;
        query = query.filter(purchases::id.eq_any(purchase_ids));
    }
    if let Some(pattern) = search.clone()
        && !item_target
    {
        query = query.filter(
            purchases::merchant.ilike(pattern.clone()).or(exists(
                purchase_items::table
                    .filter(purchase_items::purchase_id.eq(purchases::id))
                    .filter(purchase_items::name.ilike(pattern.clone())),
            )),
        );
    }
    if !item_target && (filter.minimum_amount.is_some() || filter.maximum_amount.is_some()) {
        let totals = purchase_items::table
            .inner_join(purchases::table.on(purchases::id.eq(purchase_items::purchase_id)))
            .filter(purchases::household_id.eq(filter.household_id))
            .select((purchase_items::purchase_id, purchase_items::amount_yen))
            .load::<(Uuid, i64)>(&mut conn)
            .await?;
        let mut totals_by_purchase = std::collections::HashMap::<Uuid, i128>::new();
        for (id, amount) in totals {
            *totals_by_purchase.entry(id).or_default() += i128::from(amount);
        }
        let matching_ids = totals_by_purchase
            .into_iter()
            .filter_map(|(id, total)| {
                (filter
                    .minimum_amount
                    .is_none_or(|min| total >= i128::from(min))
                    && filter
                        .maximum_amount
                        .is_none_or(|max| total <= i128::from(max)))
                .then_some(id)
            })
            .collect::<Vec<_>>();
        query = query.filter(purchases::id.eq_any(matching_ids));
    }

    let matching_purchase_ids = query.select(purchases::id).load::<Uuid>(&mut conn).await?;
    let total_count =
        i64::try_from(matching_purchase_ids.len()).expect("PostgreSQL row count fits in i64");
    let mut query = purchases::table
        .filter(purchases::household_id.eq(filter.household_id))
        .filter(purchases::id.eq_any(matching_purchase_ids))
        .into_boxed::<Pg>();
    let sync_cursor = household_events::table
        .filter(household_events::household_id.eq(filter.household_id))
        .select(max(household_events::stream_cursor))
        .first::<Option<i64>>(&mut conn)
        .await?
        .unwrap_or_default();
    if let Some(after) = after {
        let (occurred_at, id) = resolve_cursor(&mut conn, filter.household_id, after).await?;
        query = query.filter(
            purchases::occurred_at
                .lt(occurred_at)
                .or(purchases::occurred_at
                    .eq(occurred_at)
                    .and(purchases::id.lt(id))),
        );
    }
    if let Some(before) = before {
        let (occurred_at, id) = resolve_cursor(&mut conn, filter.household_id, before).await?;
        query = query.filter(
            purchases::occurred_at
                .gt(occurred_at)
                .or(purchases::occurred_at
                    .eq(occurred_at)
                    .and(purchases::id.gt(id))),
        );
    }
    let limit = i64::try_from(page_size + 1).expect("page size is bounded");
    let mut rows = if backward {
        query
            .order_by(purchases::occurred_at.asc())
            .then_order_by(purchases::id.asc())
            .limit(limit)
            .select((purchases::id, purchases::occurred_at))
            .load::<(Uuid, DateTime<Utc>)>(&mut conn)
            .await?
    } else {
        query
            .order_by(purchases::occurred_at.desc())
            .then_order_by(purchases::id.desc())
            .limit(limit)
            .select((purchases::id, purchases::occurred_at))
            .load::<(Uuid, DateTime<Utc>)>(&mut conn)
            .await?
    };
    let has_extra = rows.len() > page_size;
    if has_extra {
        rows.truncate(page_size);
    }
    if backward {
        rows.reverse();
    }
    Ok(PurchaseSearchPage {
        rows: rows
            .into_iter()
            .map(|(id, occurred_at)| PurchaseSearchRow { id, occurred_at })
            .collect(),
        total_count,
        sync_cursor,
        has_extra,
        backward,
    })
}

async fn resolve_cursor(
    conn: &mut diesel_async::AsyncPgConnection,
    household_id: Uuid,
    cursor: PurchaseSearchCursor,
) -> Result<(DateTime<Utc>, Uuid), PurchaseSearchQueryError> {
    if let Some(occurred_at) = cursor.occurred_at {
        return Ok((occurred_at, cursor.id));
    }
    use public::purchases::dsl as purchases;
    let occurred_at = purchases::purchases
        .filter(purchases::household_id.eq(household_id))
        .filter(purchases::id.eq(cursor.id))
        .select(purchases::occurred_at)
        .first::<DateTime<Utc>>(conn)
        .await
        .optional()?
        .ok_or(PurchaseSearchQueryError::CursorNotFound)?;
    Ok((occurred_at, cursor.id))
}

async fn require_member(
    conn: &mut diesel_async::AsyncPgConnection,
    actor_id: Uuid,
    household_id: Uuid,
) -> Result<(), PurchaseSearchQueryError> {
    use public::household_members;
    let active = diesel::select(exists(
        household_members::table
            .filter(household_members::household_id.eq(household_id))
            .filter(household_members::member_id.eq(actor_id))
            .filter(household_members::left_at.is_null()),
    ))
    .get_result::<bool>(conn)
    .await?;
    if active {
        Ok(())
    } else {
        Err(PurchaseSearchQueryError::HouseholdNotFound)
    }
}

fn page_size(
    first: Option<i32>,
    last: Option<i32>,
) -> Result<(usize, bool), PurchaseSearchQueryError> {
    if first.is_some() && last.is_some() {
        return Err(PurchaseSearchQueryError::InvalidPagination);
    }
    let backward = last.is_some();
    let value = last.or(first).unwrap_or(50);
    if !(1..=100).contains(&value) {
        return Err(PurchaseSearchQueryError::InvalidPagination);
    }
    Ok((
        usize::try_from(value).expect("page size is positive"),
        backward,
    ))
}

fn escape_like_pattern(value: &str) -> String {
    value
        .replace('\\', "\\\\")
        .replace('%', "\\%")
        .replace('_', "\\_")
}
