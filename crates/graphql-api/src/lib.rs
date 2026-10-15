use async_graphql::{
    Context, EmptySubscription, Enum, Error, ID, Interface, Object, Result,
    Schema as GraphqlSchema, SimpleObject, Union,
};
use base64::{Engine, engine::general_purpose::URL_SAFE_NO_PAD};
use horse_postgres::{PgPool, mail_forwarding as postgres_mail_forwarding};
use household_workflows::mail_forwarding;
use uuid::Uuid;

pub mod account_balances;
pub mod account_creation;
pub mod account_history;
pub mod account_mutations;
pub mod allocation_rule_query;
pub mod card_last4;
pub mod catalog_mutations;
pub mod catalog_queries;
pub mod category;
pub mod financial_mutations;
pub mod funding;
pub mod funding_schedule_query;
pub mod home_summary_query;
pub mod household_lifecycle;
pub mod household_membership;
pub mod household_query;
pub mod ledger_entry;
pub mod ledger_query;
pub mod member_pool;
pub mod monthly_spending_query;
pub mod pagination;
pub mod policy_mutations;
pub mod pool_mutations;
pub mod purchase_analytics_query;
pub mod purchase_correction_query;
pub mod purchase_corrections_mutations;
pub mod purchase_defaults_query;
pub mod purchases_query;
pub mod purchase_edit_mutations;
pub mod purchase_mutations;
pub mod scalars;
pub mod settlement_mutations;
pub mod shared_purchase_draft;
pub mod statement_link_mutations;
pub mod viewer_query;
pub use scalars::{DateTime, Long};

pub type Schema = GraphqlSchema<QueryRoot, MutationRoot, EmptySubscription>;

#[derive(Clone, Debug)]
pub struct Config {
    pub mailbox_domain: String,
    pub local_part_prefix: String,
    pub mail_forwarding_enabled: bool,
}

#[derive(Clone, Copy, Debug)]
pub struct Actor(pub Uuid);

pub fn schema(pool: PgPool, config: Config) -> Schema {
    GraphqlSchema::build(QueryRoot, MutationRoot, EmptySubscription)
        .data(pool)
        .data(config)
        .register_output_type::<MutationError>()
        .finish()
}

pub struct QueryRoot;

#[Object]
impl QueryRoot {
    async fn viewer(&self, ctx: &Context<'_>) -> Result<viewer_query::Viewer> {
        viewer_query::graphql_viewer(ctx).await
    }

    async fn tags(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        #[graphql(default = false)] include_archived: bool,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<catalog_queries::TagConnection> {
        catalog_queries::graphql_tags(
            ctx,
            household_id,
            include_archived,
            after,
            before,
            first,
            last,
        )
        .await
    }

    async fn allocation_rules(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<allocation_rule_query::AllocationRuleConnection> {
        allocation_rule_query::graphql_allocation_rules(
            ctx,
            household_id,
            after,
            before,
            first,
            last,
        )
        .await
    }

    async fn product_suggestions(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        query: String,
        merchant: Option<String>,
        #[graphql(default = 8)] first: i32,
    ) -> Result<Vec<catalog_queries::ProductSuggestion>> {
        catalog_queries::graphql_product_suggestions(ctx, household_id, query, merchant, first)
            .await
    }

    async fn product_catalog(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<catalog_queries::ProductConceptConnection> {
        catalog_queries::graphql_product_catalog(ctx, household_id, after, before, first, last)
            .await
    }

    async fn settlement(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        period: policy_mutations::YearMonth,
    ) -> Result<settlement_mutations::Settlement> {
        settlement_mutations::settlement(ctx, household_id, period).await
    }

    async fn loan_balances(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<settlement_mutations::LoanBalanceConnection> {
        settlement_mutations::loan_balances(ctx, household_id, after, before, first, last).await
    }

    async fn monthly_category_spending(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        from: policy_mutations::YearMonth,
        to: policy_mutations::YearMonth,
    ) -> Result<Vec<monthly_spending_query::MonthlyCategorySpending>> {
        monthly_spending_query::graphql_monthly_category_spending(ctx, household_id, from, to).await
    }

    async fn monthly_member_spending(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        from: policy_mutations::YearMonth,
        to: policy_mutations::YearMonth,
    ) -> Result<monthly_spending_query::MonthlyMemberSpendingReport> {
        monthly_spending_query::graphql_monthly_member_spending(ctx, household_id, from, to).await
    }

    async fn funding_schedules(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        active: Option<bool>,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<funding_schedule_query::FundingScheduleConnection> {
        funding_schedule_query::graphql_funding_schedules(
            ctx,
            household_id,
            active,
            after,
            before,
            first,
            last,
        )
        .await
    }

    async fn account_balances(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
    ) -> Result<Vec<account_balances::AccountBalance>> {
        account_balances::resolve_account_balances(ctx, household_id).await
    }

    async fn account_movements(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<account_history::AccountMovementConnection> {
        account_history::graphql_account_movements(ctx, household_id, after, before, first, last)
            .await
    }

    async fn account_incomes(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<account_history::AccountIncomeConnection> {
        account_history::graphql_account_incomes(ctx, household_id, after, before, first, last)
            .await
    }

    async fn household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
    ) -> Result<Option<HouseholdMailForwardingStatus>> {
        let actor = actor(ctx)?;
        let household_id = decode_household_id(&household_id)?;
        match postgres_mail_forwarding::active(
            ctx.data::<PgPool>()?,
            mail_forwarding::Command {
                actor_id: actor.0.to_string(),
                household_id: household_id.to_string(),
            },
        )
        .await
        {
            Ok(status) => Ok(Some(status.into())),
            Err(postgres_mail_forwarding::MailForwardingError::ForwardingNotFound) => Ok(None),
            Err(postgres_mail_forwarding::MailForwardingError::HouseholdNotFound) => Ok(None),
            Err(error) => Err(internal_error(error)),
        }
    }

    async fn budgets(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        period: policy_mutations::YearMonth,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<policy_mutations::BudgetConnection> {
        policy_mutations::graphql_budgets(ctx, household_id, period, after, before, first, last)
            .await
    }

    async fn recurring_expenses(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        #[graphql(default = false)] include_archived: bool,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<policy_mutations::RecurringExpenseConnection> {
        policy_mutations::graphql_recurring_expenses(
            ctx,
            household_id,
            include_archived,
            after,
            before,
            first,
            last,
        )
        .await
    }

    async fn household_invitations(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<household_lifecycle::HouseholdInvitationConnection> {
        household_lifecycle::household_invitations(ctx, household_id, after, before, first, last)
            .await
    }

    async fn households(&self, ctx: &Context<'_>) -> Result<Vec<household_lifecycle::Household>> {
        household_query::graphql_households(ctx).await
    }

    async fn household(
        &self,
        ctx: &Context<'_>,
        id: ID,
    ) -> Result<Option<household_lifecycle::Household>> {
        household_query::graphql_household(ctx, id).await
    }

    async fn home_summary(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
    ) -> Result<home_summary_query::HomeSummary> {
        home_summary_query::graphql_home_summary(ctx, household_id).await
    }

    async fn purchase_defaults(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
    ) -> Result<Vec<purchase_defaults_query::PurchaseDefault>> {
        purchase_defaults_query::graphql_purchase_defaults(ctx, household_id).await
    }

    async fn purchase_corrections(
        &self,
        ctx: &Context<'_>,
        purchase_id: ID,
        first: Option<i32>,
        after: Option<String>,
    ) -> Result<purchase_mutations::PurchaseCorrectionConnection> {
        purchase_correction_query::graphql_purchase_corrections(ctx, purchase_id, first, after)
            .await
    }

    async fn shared_purchase_draft(
        &self,
        ctx: &Context<'_>,
        id: ID,
    ) -> Result<Option<shared_purchase_draft::SharedPurchaseDraft>> {
        shared_purchase_draft::shared_purchase_draft(ctx, id).await
    }

    async fn purchase_analytics(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        from: policy_mutations::YearMonth,
        to: policy_mutations::YearMonth,
    ) -> Result<Vec<purchase_analytics_query::PurchaseAnalyticsEntry>> {
        purchase_analytics_query::graphql_purchase_analytics(ctx, household_id, from, to).await
    }

    async fn purchase_price_observations(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        from: policy_mutations::YearMonth,
        to: policy_mutations::YearMonth,
        item_ids: Vec<String>,
    ) -> Result<Vec<purchase_analytics_query::PurchasePriceObservation>> {
        purchase_analytics_query::graphql_purchase_price_observations(
            ctx,
            household_id,
            from,
            to,
            item_ids,
        )
        .await
    }

    async fn purchases(
        &self,
        ctx: &Context<'_>,
        filter: purchases_query::PurchaseFilter,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<purchases_query::PurchaseConnection> {
        purchases_query::graphql_purchases(ctx, filter, after, before, first, last).await
    }

    async fn ledger(
        &self,
        ctx: &Context<'_>,
        household_id: ID,
        after: Option<String>,
        before: Option<String>,
        first: Option<i32>,
        last: Option<i32>,
    ) -> Result<ledger_query::LedgerConnection> {
        ledger_query::graphql_ledger(ctx, household_id, after, before, first, last).await
    }
}

pub struct MutationRoot;

#[Object]
impl MutationRoot {
    async fn issue_household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        input: HouseholdMailForwardingInput,
    ) -> Result<IssueHouseholdMailForwardingResult> {
        let config = ctx.data::<Config>()?;
        if !config.mail_forwarding_enabled {
            return Ok(HouseholdMailForwardingUserError::disabled(
                "メール転送先の発行は現在利用できません。",
            )
            .into());
        }
        let actor = actor(ctx)?;
        match issue_or_rotate(ctx, actor, input, false).await {
            Ok(address) => Ok(IssueHouseholdMailForwardingSuccess { result: address }.into()),
            Err(error) => Ok(error.into()),
        }
    }

    async fn rotate_household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        input: HouseholdMailForwardingInput,
    ) -> Result<RotateHouseholdMailForwardingResult> {
        let config = ctx.data::<Config>()?;
        if !config.mail_forwarding_enabled {
            return Ok(HouseholdMailForwardingUserError::disabled(
                "メール転送先の交換は現在利用できません。",
            )
            .into());
        }
        let actor = actor(ctx)?;
        match issue_or_rotate(ctx, actor, input, true).await {
            Ok(address) => Ok(RotateHouseholdMailForwardingSuccess { result: address }.into()),
            Err(error) => Ok(error.into()),
        }
    }

    async fn revoke_household_mail_forwarding(
        &self,
        ctx: &Context<'_>,
        input: HouseholdMailForwardingInput,
    ) -> Result<RevokeHouseholdMailForwardingResult> {
        let actor = actor(ctx)?;
        let household_id = match decode_household_id(&input.household_id) {
            Ok(value) => value,
            Err(_) => return Ok(bad_input_error().into()),
        };
        match postgres_mail_forwarding::revoke(
            ctx.data::<PgPool>()?,
            mail_forwarding::Command {
                actor_id: actor.0.to_string(),
                household_id: household_id.to_string(),
            },
        )
        .await
        {
            Ok(status) => Ok(RevokeHouseholdMailForwardingSuccess {
                result: status.into(),
            }
            .into()),
            Err(error) => match mutation_user_error(&error) {
                Some(error) => Ok(error.into()),
                None => Err(internal_error(error)),
            },
        }
    }

    async fn create_category(
        &self,
        ctx: &Context<'_>,
        input: category::CreateCategoryInput,
    ) -> Result<category::CreateCategoryResult> {
        category::create_category(ctx, input).await
    }

    async fn update_category_name(
        &self,
        ctx: &Context<'_>,
        input: category::UpdateCategoryNameInput,
    ) -> Result<category::UpdateCategoryNameResult> {
        category::update_category_name(ctx, input).await
    }

    async fn set_category_archived(
        &self,
        ctx: &Context<'_>,
        input: category::SetCategoryArchivedInput,
    ) -> Result<category::SetCategoryArchivedResult> {
        category::set_category_archived(ctx, input).await
    }

    async fn record_account_movement(
        &self,
        ctx: &Context<'_>,
        input: account_mutations::RecordAccountMovementInput,
    ) -> Result<account_mutations::RecordAccountMovementResult> {
        account_mutations::graphql_record_account_movement(ctx, input).await
    }

    async fn record_external_income(
        &self,
        ctx: &Context<'_>,
        input: account_mutations::RecordExternalIncomeInput,
    ) -> Result<account_mutations::RecordExternalIncomeResult> {
        account_mutations::graphql_record_external_income(ctx, input).await
    }

    async fn create_account(
        &self,
        ctx: &Context<'_>,
        input: account_creation::CreateAccountInput,
    ) -> Result<account_creation::CreateAccountResult> {
        account_creation::create_account(ctx, input).await
    }

    async fn record_funding(
        &self,
        ctx: &Context<'_>,
        input: funding::RecordFundingInput,
    ) -> Result<funding::RecordFundingResult> {
        funding::record_funding(ctx, input).await
    }

    async fn leave_household(
        &self,
        ctx: &Context<'_>,
        input: household_membership::LeaveHouseholdInput,
    ) -> Result<household_membership::LeaveHouseholdResult> {
        household_membership::leave_household(ctx, input).await
    }

    async fn remove_household_member(
        &self,
        ctx: &Context<'_>,
        input: household_membership::RemoveHouseholdMemberInput,
    ) -> Result<household_membership::RemoveHouseholdMemberResult> {
        household_membership::remove_household_member(ctx, input).await
    }

    async fn transfer_household_owner(
        &self,
        ctx: &Context<'_>,
        input: household_membership::TransferHouseholdOwnerInput,
    ) -> Result<household_membership::TransferHouseholdOwnerResult> {
        household_membership::transfer_household_owner(ctx, input).await
    }

    async fn create_tag(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::CreateTagInput,
    ) -> Result<catalog_mutations::CreateTagResult> {
        catalog_mutations::create_tag(ctx, input).await
    }

    async fn update_tag(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::UpdateTagInput,
    ) -> Result<catalog_mutations::UpdateTagResult> {
        catalog_mutations::update_tag(ctx, input).await
    }

    async fn archive_tag(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::ArchiveTagInput,
    ) -> Result<catalog_mutations::ArchiveTagResult> {
        catalog_mutations::archive_tag(ctx, input).await
    }

    async fn create_product_concept(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::CreateProductConceptInput,
    ) -> Result<catalog_mutations::CreateProductConceptResult> {
        catalog_mutations::create_product_concept(ctx, input).await
    }

    async fn create_product_variant(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::CreateProductVariantInput,
    ) -> Result<catalog_mutations::CreateProductVariantResult> {
        catalog_mutations::create_product_variant(ctx, input).await
    }

    async fn set_product_alias(
        &self,
        ctx: &Context<'_>,
        input: catalog_mutations::SetProductAliasInput,
    ) -> Result<catalog_mutations::SetProductAliasResult> {
        catalog_mutations::set_product_alias(ctx, input).await
    }

    async fn set_account_card_last4(
        &self,
        ctx: &Context<'_>,
        input: card_last4::SetAccountCardLast4Input,
    ) -> Result<card_last4::SetAccountCardLast4Result> {
        card_last4::set_account_card_last4(ctx, input).await
    }

    async fn create_budget(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::CreateBudgetInput,
    ) -> Result<policy_mutations::CreateBudgetResult> {
        policy_mutations::create_budget(ctx, input).await
    }

    async fn update_budget(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::UpdateBudgetInput,
    ) -> Result<policy_mutations::UpdateBudgetResult> {
        policy_mutations::update_budget(ctx, input).await
    }

    async fn archive_budget(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::ArchiveBudgetInput,
    ) -> Result<policy_mutations::ArchiveBudgetResult> {
        policy_mutations::archive_budget(ctx, input).await
    }

    async fn create_recurring_expense(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::CreateRecurringExpenseInput,
    ) -> Result<policy_mutations::CreateRecurringExpenseResult> {
        policy_mutations::create_recurring_expense(ctx, input).await
    }

    async fn update_recurring_expense(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::UpdateRecurringExpenseInput,
    ) -> Result<policy_mutations::UpdateRecurringExpenseResult> {
        policy_mutations::update_recurring_expense(ctx, input).await
    }

    async fn archive_recurring_expense(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::ArchiveRecurringExpenseInput,
    ) -> Result<policy_mutations::ArchiveRecurringExpenseResult> {
        policy_mutations::archive_recurring_expense(ctx, input).await
    }

    async fn match_recurring_expense(
        &self,
        ctx: &Context<'_>,
        input: policy_mutations::MatchRecurringExpenseInput,
    ) -> Result<policy_mutations::MatchRecurringExpenseResult> {
        policy_mutations::match_recurring_expense(ctx, input).await
    }

    async fn create_household(
        &self,
        ctx: &Context<'_>,
        input: household_lifecycle::CreateHouseholdInput,
    ) -> Result<household_lifecycle::CreateHouseholdResult> {
        household_lifecycle::create_household(ctx, input).await
    }

    async fn create_household_invitation(
        &self,
        ctx: &Context<'_>,
        input: household_lifecycle::CreateHouseholdInvitationInput,
    ) -> Result<household_lifecycle::CreateHouseholdInvitationResult> {
        household_lifecycle::create_household_invitation(ctx, input).await
    }

    async fn accept_household_invitation(
        &self,
        ctx: &Context<'_>,
        input: household_lifecycle::AcceptHouseholdInvitationInput,
    ) -> Result<household_lifecycle::AcceptHouseholdInvitationResult> {
        household_lifecycle::accept_household_invitation(ctx, input).await
    }

    async fn revoke_household_invitation(
        &self,
        ctx: &Context<'_>,
        input: household_lifecycle::RevokeHouseholdInvitationInput,
    ) -> Result<household_lifecycle::RevokeHouseholdInvitationResult> {
        household_lifecycle::revoke_household_invitation(ctx, input).await
    }

    async fn record_transfer(
        &self,
        ctx: &Context<'_>,
        input: financial_mutations::RecordTransferInput,
    ) -> Result<financial_mutations::RecordTransferResult> {
        financial_mutations::record_transfer(ctx, input).await
    }

    async fn record_repayment(
        &self,
        ctx: &Context<'_>,
        input: financial_mutations::RecordRepaymentInput,
    ) -> Result<financial_mutations::RecordRepaymentResult> {
        financial_mutations::record_repayment(ctx, input).await
    }

    async fn create_purchase(
        &self,
        ctx: &Context<'_>,
        input: purchase_mutations::CreatePurchaseInput,
    ) -> Result<purchase_mutations::CreatePurchaseResult> {
        purchase_mutations::create_purchase(ctx, input).await
    }

    async fn create_and_post_purchase(
        &self,
        ctx: &Context<'_>,
        input: purchase_mutations::CreatePurchaseInput,
    ) -> Result<purchase_mutations::CreateAndPostPurchaseResult> {
        purchase_mutations::create_and_post_purchase(ctx, input).await
    }

    async fn post_purchase(
        &self,
        ctx: &Context<'_>,
        input: purchase_mutations::PostPurchaseInput,
    ) -> Result<purchase_mutations::PostPurchaseResult> {
        purchase_mutations::post_purchase(ctx, input).await
    }

    async fn update_purchase(
        &self,
        ctx: &Context<'_>,
        input: purchase_edit_mutations::UpdatePurchaseInput,
    ) -> Result<purchase_edit_mutations::UpdatePurchaseResult> {
        purchase_edit_mutations::update_purchase(ctx, input).await
    }

    async fn update_purchase_item(
        &self,
        ctx: &Context<'_>,
        input: purchase_edit_mutations::UpdatePurchaseItemInput,
    ) -> Result<purchase_edit_mutations::UpdatePurchaseItemResult> {
        purchase_edit_mutations::update_purchase_item(ctx, input).await
    }

    async fn bulk_update_purchase_items(
        &self,
        ctx: &Context<'_>,
        input: purchase_edit_mutations::BulkUpdatePurchaseItemsInput,
    ) -> Result<purchase_edit_mutations::BulkUpdatePurchaseItemsResult> {
        purchase_edit_mutations::bulk_update_purchase_items(ctx, input).await
    }

    async fn bulk_delete_purchase_items(
        &self,
        ctx: &Context<'_>,
        input: purchase_edit_mutations::BulkDeletePurchaseItemsInput,
    ) -> Result<purchase_edit_mutations::BulkDeletePurchaseItemsResult> {
        purchase_edit_mutations::bulk_delete_purchase_items(ctx, input).await
    }

    async fn create_pool(
        &self,
        ctx: &Context<'_>,
        input: pool_mutations::CreatePoolInput,
    ) -> Result<pool_mutations::CreatePoolResult> {
        pool_mutations::create_pool(ctx, input).await
    }

    async fn set_pool_funding_rule(
        &self,
        ctx: &Context<'_>,
        input: pool_mutations::SetPoolFundingRuleInput,
    ) -> Result<pool_mutations::SetPoolFundingRuleResult> {
        pool_mutations::set_pool_funding_rule(ctx, input).await
    }

    async fn set_default_allocation(
        &self,
        ctx: &Context<'_>,
        input: pool_mutations::SetDefaultAllocationInput,
    ) -> Result<pool_mutations::SetDefaultAllocationResult> {
        pool_mutations::set_default_allocation(ctx, input).await
    }

    async fn set_allocation_rule(
        &self,
        ctx: &Context<'_>,
        input: pool_mutations::SetAllocationRuleInput,
    ) -> Result<pool_mutations::SetAllocationRuleResult> {
        pool_mutations::set_allocation_rule(ctx, input).await
    }

    async fn set_funding_schedule(
        &self,
        ctx: &Context<'_>,
        input: pool_mutations::SetFundingScheduleInput,
    ) -> Result<pool_mutations::SetFundingScheduleResult> {
        pool_mutations::set_funding_schedule(ctx, input).await
    }

    async fn close_period(
        &self,
        ctx: &Context<'_>,
        input: settlement_mutations::ClosePeriodInput,
    ) -> Result<settlement_mutations::ClosePeriodResult> {
        settlement_mutations::close_period(ctx, input).await
    }

    async fn create_shared_purchase_draft(
        &self,
        ctx: &Context<'_>,
        input: shared_purchase_draft::CreateSharedPurchaseDraftInput,
    ) -> Result<shared_purchase_draft::CreateSharedPurchaseDraftResult> {
        shared_purchase_draft::create_shared_purchase_draft(ctx, input).await
    }

    async fn update_shared_purchase_draft(
        &self,
        ctx: &Context<'_>,
        input: shared_purchase_draft::UpdateSharedPurchaseDraftInput,
    ) -> Result<shared_purchase_draft::UpdateSharedPurchaseDraftResult> {
        shared_purchase_draft::update_shared_purchase_draft(ctx, input).await
    }

    async fn link_statement_account_movement(
        &self,
        ctx: &Context<'_>,
        input: statement_link_mutations::LinkStatementAccountMovementInput,
    ) -> Result<statement_link_mutations::LinkStatementAccountMovementResult> {
        statement_link_mutations::link_statement_account_movement(ctx, input).await
    }

    async fn link_statement_account_income(
        &self,
        ctx: &Context<'_>,
        input: statement_link_mutations::LinkStatementAccountIncomeInput,
    ) -> Result<statement_link_mutations::LinkStatementAccountIncomeResult> {
        statement_link_mutations::link_statement_account_income(ctx, input).await
    }

    async fn link_statement_transfer(
        &self,
        ctx: &Context<'_>,
        input: statement_link_mutations::LinkStatementTransferInput,
    ) -> Result<statement_link_mutations::LinkStatementTransferResult> {
        statement_link_mutations::link_statement_transfer(ctx, input).await
    }

    async fn link_statement_refund_correction(
        &self,
        ctx: &Context<'_>,
        input: statement_link_mutations::LinkStatementRefundCorrectionInput,
    ) -> Result<statement_link_mutations::LinkStatementRefundCorrectionResult> {
        statement_link_mutations::link_statement_refund_correction(ctx, input).await
    }

    async fn correct_purchase(
        &self,
        ctx: &Context<'_>,
        input: purchase_corrections_mutations::CorrectPurchaseInput,
    ) -> Result<purchase_corrections_mutations::CorrectPurchaseResult> {
        purchase_corrections_mutations::correct_purchase(ctx, input).await
    }

    async fn correct_purchase_from_statement(
        &self,
        ctx: &Context<'_>,
        input: purchase_corrections_mutations::CorrectPurchaseFromStatementInput,
    ) -> Result<purchase_corrections_mutations::CorrectPurchaseResult> {
        purchase_corrections_mutations::correct_purchase_from_statement(ctx, input).await
    }
}

async fn issue_or_rotate(
    ctx: &Context<'_>,
    actor: Actor,
    input: HouseholdMailForwardingInput,
    rotate: bool,
) -> Result<HouseholdMailForwardingAddress, HouseholdMailForwardingUserError> {
    let household_id = decode_household_id(&input.household_id).map_err(|_| bad_input_error())?;
    let config = ctx.data::<Config>().map_err(|_| bad_input_error())?;
    let command = mail_forwarding::IssueCommand {
        command: mail_forwarding::Command {
            actor_id: actor.0.to_string(),
            household_id: household_id.to_string(),
        },
        mailbox_domain: config.mailbox_domain.clone(),
        local_part_prefix: config.local_part_prefix.clone(),
    };
    let result = if rotate {
        postgres_mail_forwarding::rotate(
            ctx.data::<PgPool>().map_err(|_| bad_input_error())?,
            command,
        )
        .await
    } else {
        postgres_mail_forwarding::issue(
            ctx.data::<PgPool>().map_err(|_| bad_input_error())?,
            command,
        )
        .await
    };
    result
        .map(|address| HouseholdMailForwardingAddress {
            address: address.address,
            status: HouseholdMailForwardingStatus {
                created_at: address.created_at.into(),
                active: true,
            },
        })
        .map_err(|error| mutation_user_error(&error).unwrap_or_else(bad_input_error))
}

pub(crate) fn actor(ctx: &Context<'_>) -> Result<Actor> {
    ctx.data::<Actor>()
        .copied()
        .map_err(|_| Error::new("authentication required"))
}

pub(crate) fn decode_household_id(id: &ID) -> Result<Uuid> {
    let bytes = URL_SAFE_NO_PAD
        .decode(id.as_str())
        .map_err(|_| Error::new("invalid Household node ID"))?;
    let separator = bytes
        .iter()
        .position(|byte| *byte == b':')
        .ok_or_else(|| Error::new("invalid Household node ID"))?;
    let (kind, payload_with_separator) = bytes.split_at(separator);
    let payload = &payload_with_separator[1..];
    if kind != b"Household" || payload.len() != 16 {
        return Err(Error::new("invalid Household node ID"));
    }
    let id = Uuid::from_slice(payload).map_err(|_| Error::new("invalid Household node ID"))?;
    if id.is_nil() {
        return Err(Error::new("invalid Household node ID"));
    }
    Ok(id)
}

fn mutation_user_error(
    error: &postgres_mail_forwarding::MailForwardingError,
) -> Option<HouseholdMailForwardingUserError> {
    use postgres_mail_forwarding::MailForwardingError as E;
    match error {
        E::InvalidUuid(_) | E::Workflow(_) => Some(bad_input_error()),
        E::ForwardingNotFound | E::HouseholdNotFound => Some(HouseholdMailForwardingUserError {
            code: MutationErrorCode::NotFound,
            message: "対象が見つかりません。".to_owned(),
        }),
        E::Conflict => Some(HouseholdMailForwardingUserError {
            code: MutationErrorCode::Conflict,
            message: "最新の状態を読み込んでから再試行してください。".to_owned(),
        }),
        E::Pool(_) | E::Database(_) => None,
    }
}

fn bad_input_error() -> HouseholdMailForwardingUserError {
    HouseholdMailForwardingUserError {
        code: MutationErrorCode::BadInput,
        message: "入力を確認してください。".to_owned(),
    }
}

pub(crate) fn internal_error(error: impl std::fmt::Debug) -> Error {
    tracing::error!(error = ?error, "GraphQL operation failed");
    Error::new("internal server error")
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdMailForwardingStatus {
    created_at: DateTime,
    active: bool,
}

impl From<mail_forwarding::Status> for HouseholdMailForwardingStatus {
    fn from(value: mail_forwarding::Status) -> Self {
        Self {
            created_at: value.created_at.into(),
            active: value.active,
        }
    }
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdMailForwardingAddress {
    address: String,
    status: HouseholdMailForwardingStatus,
}

#[derive(Clone, Debug, async_graphql::InputObject)]
pub struct HouseholdMailForwardingInput {
    household_id: ID,
}

#[derive(Clone, Copy, Debug, Enum, Eq, PartialEq)]
pub enum MutationErrorCode {
    BadInput,
    Conflict,
    NotFound,
    MailForwardingDisabled,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct HouseholdMailForwardingUserError {
    code: MutationErrorCode,
    message: String,
}

impl HouseholdMailForwardingUserError {
    fn disabled(message: &str) -> Self {
        Self {
            code: MutationErrorCode::MailForwardingDisabled,
            message: message.to_owned(),
        }
    }
}

#[derive(Interface)]
#[graphql(
    field(name = "code", ty = "&MutationErrorCode"),
    field(name = "message", ty = "&String")
)]
pub enum MutationError {
    HouseholdMailForwardingUserError(HouseholdMailForwardingUserError),
    CreateCategoryUserError(category::CreateCategoryUserError),
    UpdateCategoryNameUserError(category::UpdateCategoryNameUserError),
    SetCategoryArchivedUserError(category::SetCategoryArchivedUserError),
    RecordAccountMovementUserError(account_mutations::RecordAccountMovementUserError),
    RecordExternalIncomeUserError(account_mutations::RecordExternalIncomeUserError),
    CreateAccountUserError(account_creation::CreateAccountUserError),
    RecordFundingUserError(funding::RecordFundingUserError),
    LeaveHouseholdUserError(household_membership::LeaveHouseholdUserError),
    RemoveHouseholdMemberUserError(household_membership::RemoveHouseholdMemberUserError),
    TransferHouseholdOwnerUserError(household_membership::TransferHouseholdOwnerUserError),
    CreateTagUserError(catalog_mutations::CreateTagUserError),
    UpdateTagUserError(catalog_mutations::UpdateTagUserError),
    ArchiveTagUserError(catalog_mutations::ArchiveTagUserError),
    CreateProductConceptUserError(catalog_mutations::CreateProductConceptUserError),
    CreateProductVariantUserError(catalog_mutations::CreateProductVariantUserError),
    SetProductAliasUserError(catalog_mutations::SetProductAliasUserError),
    SetAccountCardLast4UserError(card_last4::SetAccountCardLast4UserError),
    CreateBudgetUserError(policy_mutations::CreateBudgetUserError),
    UpdateBudgetUserError(policy_mutations::UpdateBudgetUserError),
    ArchiveBudgetUserError(policy_mutations::ArchiveBudgetUserError),
    CreateRecurringExpenseUserError(policy_mutations::CreateRecurringExpenseUserError),
    UpdateRecurringExpenseUserError(policy_mutations::UpdateRecurringExpenseUserError),
    ArchiveRecurringExpenseUserError(policy_mutations::ArchiveRecurringExpenseUserError),
    MatchRecurringExpenseUserError(policy_mutations::MatchRecurringExpenseUserError),
    CreateHouseholdUserError(household_lifecycle::CreateHouseholdUserError),
    CreateHouseholdInvitationUserError(household_lifecycle::CreateHouseholdInvitationUserError),
    AcceptHouseholdInvitationUserError(household_lifecycle::AcceptHouseholdInvitationUserError),
    RevokeHouseholdInvitationUserError(household_lifecycle::RevokeHouseholdInvitationUserError),
    RecordTransferUserError(financial_mutations::RecordTransferUserError),
    RecordRepaymentUserError(financial_mutations::RecordRepaymentUserError),
    CreatePurchaseUserError(purchase_mutations::CreatePurchaseUserError),
    CreateAndPostPurchaseUserError(purchase_mutations::CreateAndPostPurchaseUserError),
    PostPurchaseUserError(purchase_mutations::PostPurchaseUserError),
    UpdatePurchaseUserError(purchase_edit_mutations::UpdatePurchaseUserError),
    UpdatePurchaseItemUserError(purchase_edit_mutations::UpdatePurchaseItemUserError),
    BulkUpdatePurchaseItemsUserError(purchase_edit_mutations::BulkUpdatePurchaseItemsUserError),
    BulkDeletePurchaseItemsUserError(purchase_edit_mutations::BulkDeletePurchaseItemsUserError),
    CreatePoolUserError(pool_mutations::CreatePoolUserError),
    SetPoolFundingRuleUserError(pool_mutations::SetPoolFundingRuleUserError),
    SetDefaultAllocationUserError(pool_mutations::SetDefaultAllocationUserError),
    SetAllocationRuleUserError(pool_mutations::SetAllocationRuleUserError),
    SetFundingScheduleUserError(pool_mutations::SetFundingScheduleUserError),
    ClosePeriodUserError(settlement_mutations::ClosePeriodUserError),
    SharedPurchaseDraftUserError(shared_purchase_draft::SharedPurchaseDraftUserError),
    UpdateSharedPurchaseDraftConflict(shared_purchase_draft::UpdateSharedPurchaseDraftConflict),
}

#[derive(Clone, Debug, SimpleObject)]
pub struct IssueHouseholdMailForwardingSuccess {
    result: HouseholdMailForwardingAddress,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RotateHouseholdMailForwardingSuccess {
    result: HouseholdMailForwardingAddress,
}

#[derive(Clone, Debug, SimpleObject)]
pub struct RevokeHouseholdMailForwardingSuccess {
    result: HouseholdMailForwardingStatus,
}

#[derive(Union)]
pub enum IssueHouseholdMailForwardingResult {
    Success(IssueHouseholdMailForwardingSuccess),
    UserError(HouseholdMailForwardingUserError),
}

#[derive(Union)]
pub enum RotateHouseholdMailForwardingResult {
    Success(RotateHouseholdMailForwardingSuccess),
    UserError(HouseholdMailForwardingUserError),
}

#[derive(Union)]
pub enum RevokeHouseholdMailForwardingResult {
    Success(RevokeHouseholdMailForwardingSuccess),
    UserError(HouseholdMailForwardingUserError),
}
