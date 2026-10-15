use diesel::{
    ExpressionMethods, OptionalExtension, PgSortExpressionMethods, QueryDsl,
    result::Error as DieselError,
};
use diesel_async::RunQueryDsl;
use uuid::Uuid;

use crate::{PgPool, pool_mutations, schema::public};

#[derive(Debug)]
pub enum AllocationRulesQueryError {
    Pool(String),
    Database(DieselError),
    HouseholdNotFound,
    MemberNotFound,
    CursorNotFound,
    Projection(pool_mutations::AllocationRuleProjectionError),
}

impl From<DieselError> for AllocationRulesQueryError {
    fn from(error: DieselError) -> Self {
        Self::Database(error)
    }
}

pub struct AllocationRulesPage {
    pub rows: Vec<pool_mutations::AllocationRuleProjection>,
    pub total_count: i64,
    pub has_next_page: bool,
    pub has_previous_page: bool,
}

pub async fn allocation_rules(
    pool: &PgPool,
    actor_id: Uuid,
    household_id: Uuid,
    after_id: Option<Uuid>,
    before_id: Option<Uuid>,
    page_size: usize,
    backward: bool,
) -> Result<AllocationRulesPage, AllocationRulesQueryError> {
    use public::{allocation_rules as rules, household_members as members, households};

    let mut conn = pool
        .get()
        .await
        .map_err(|error| AllocationRulesQueryError::Pool(error.to_string()))?;
    let household_exists = households::table
        .filter(households::id.eq(household_id))
        .select(households::id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !household_exists {
        return Err(AllocationRulesQueryError::HouseholdNotFound);
    }
    let member_exists = members::table
        .filter(members::household_id.eq(household_id))
        .filter(members::member_id.eq(actor_id))
        .filter(members::left_at.is_null())
        .select(members::member_id)
        .first::<Uuid>(&mut *conn)
        .await
        .optional()?
        .is_some();
    if !member_exists {
        return Err(AllocationRulesQueryError::MemberNotFound);
    }

    let ordered_rows = rules::table
        .filter(rules::household_id.eq(household_id))
        .order_by((
            rules::category_id.asc().nulls_last(),
            rules::merchant_key.asc().nulls_last(),
            rules::id.asc(),
        ))
        .select((rules::id, rules::category_id, rules::merchant_key))
        .load::<(Uuid, Option<Uuid>, Option<String>)>(&mut *conn)
        .await?;
    drop(conn);

    let total_count = i64::try_from(ordered_rows.len()).unwrap_or(i64::MAX);
    let after_index = find_cursor_index(&ordered_rows, after_id)?;
    let before_index = find_cursor_index(&ordered_rows, before_id)?;
    let start = after_index.map_or(0, |index| index + 1);
    let end = before_index.unwrap_or(ordered_rows.len());
    let range_end = end.max(start).min(ordered_rows.len());
    let range_start = start.min(range_end);
    let selected = &ordered_rows[range_start..range_end];
    let has_extra_rows = selected.len() > page_size;
    let page_slice = if backward && has_extra_rows {
        &selected[selected.len() - page_size..]
    } else {
        &selected[..selected.len().min(page_size)]
    };
    let mut page_rows = page_slice.iter().map(|row| row.0).collect::<Vec<_>>();
    if backward {
        page_rows.reverse();
    }
    let has_previous_page = backward && has_extra_rows || !backward && after_id.is_some();
    let has_next_page = if backward {
        before_id.is_some()
    } else {
        has_extra_rows
    };
    let mut rows = Vec::with_capacity(page_rows.len());
    for id in page_rows {
        rows.push(
            pool_mutations::load_allocation_rule_projection(pool, actor_id, id)
                .await
                .map_err(AllocationRulesQueryError::Projection)?,
        );
    }
    Ok(AllocationRulesPage {
        rows,
        total_count,
        has_next_page,
        has_previous_page,
    })
}

fn find_cursor_index(
    rows: &[(Uuid, Option<Uuid>, Option<String>)],
    cursor_id: Option<Uuid>,
) -> Result<Option<usize>, AllocationRulesQueryError> {
    cursor_id
        .map(|id| {
            rows.iter()
                .position(|row| row.0 == id)
                .ok_or(AllocationRulesQueryError::CursorNotFound)
        })
        .transpose()
}
