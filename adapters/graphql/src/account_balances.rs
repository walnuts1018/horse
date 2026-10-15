use async_graphql::{Context, Error, ID, Result, SimpleObject};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use bookkeeping_postgres::account_balances as postgres_account_balances;
use horse_postgres_common::PgPool;
use household_postgres::household_access as postgres_household_access;
use uuid::Uuid;

#[derive(Clone, Debug, SimpleObject)]
pub struct AccountBalance {
    account_id: ID,
    balance_yen: crate::Long,
    currency: String,
}

pub async fn resolve_account_balances(
    ctx: &Context<'_>,
    household_node_id: ID,
) -> Result<Vec<AccountBalance>> {
    let actor = crate::actor(ctx)?;
    let household_id = crate::decode_household_id(&household_node_id)?;
    let pool = ctx.data::<PgPool>()?;

    if !postgres_household_access::is_active_member(pool, household_id, actor.0)
        .await
        .map_err(crate::internal_error)?
    {
        return Err(Error::new("not found"));
    }

    postgres_account_balances::account_balances(pool, household_id)
        .await
        .map_err(crate::internal_error)
        .map(|balances| {
            balances
                .into_iter()
                .map(|balance| AccountBalance {
                    account_id: encode_account_id(balance.account_id),
                    balance_yen: crate::Long(balance.balance_yen),
                    currency: "JPY".to_owned(),
                })
                .collect()
        })
}

fn encode_account_id(account_id: Uuid) -> ID {
    let mut payload = Vec::with_capacity(b"Account:".len() + 16);
    payload.extend_from_slice(b"Account:");
    payload.extend_from_slice(account_id.as_bytes());
    ID(URL_SAFE_NO_PAD.encode(payload))
}
