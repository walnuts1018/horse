import { graphql } from "react-relay";

export const monthlyMemberSpendingQuery = graphql`
  query MonthlyMemberSpendingQuery($householdId: ID!, $from: YearMonth!, $to: YearMonth!) {
    monthlyMemberSpending(householdId: $householdId, from: $from, to: $to) {
      items {
        period
        memberId
        memberName
        amountYen
      }
      hasUnattributedPurchases
    }
  }
`;

export const analysisPurchasePriceObservationsQuery = graphql`
  query AnalysisPurchasePriceObservationsQuery(
    $householdId: ID!
    $from: YearMonth!
    $to: YearMonth!
    $itemIds: [String!]!
  ) {
    purchasePriceObservations(householdId: $householdId, from: $from, to: $to, itemIds: $itemIds) {
      itemId
      occurredAt
      amountYen
      remainingRefundableAmountYen
      effectiveUnitPriceNumeratorYen
      effectiveUnitPriceDenominator
      printedUnitPriceYen
      printedUnitPriceQuantityNumerator
      printedUnitPriceQuantityDenominator
      printedUnitPriceUnit
    }
  }
`;

export const analysisDashboardQuery = graphql`
  query AnalysisDashboardQuery(
    $householdId: ID!
    $from: YearMonth!
    $to: YearMonth!
    $period: YearMonth!
  ) {
    monthlyCategorySpending(householdId: $householdId, from: $from, to: $to) {
      period
      categoryId
      categoryName
      amountYen
    }
    monthlyMemberSpending(householdId: $householdId, from: $from, to: $to) {
      items {
        period
        memberId
        memberName
        amountYen
      }
      hasUnattributedPurchases
    }
    purchaseAnalytics(householdId: $householdId, from: $from, to: $to) {
      dimension
      id
      name
      period
      amountYen
      count
    }
    ...AnalysisBudgetsFragment @arguments(householdId: $householdId, period: $period)
    ...AnalysisRecurringExpensesFragment @arguments(householdId: $householdId)
  }
`;

export const analysisBudgetsFragment = graphql`
  fragment AnalysisBudgetsFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    period: { type: "YearMonth!" }
    first: { type: "Int", defaultValue: 100 }
    after: { type: "String" }
  )
  @refetchable(queryName: "AnalysisBudgetsPaginationQuery") {
    budgets(householdId: $householdId, period: $period, first: $first, after: $after)
      @connection(key: "AnalysisBudgetsFragment_budgets", filters: ["householdId", "period"]) {
      edges {
        node {
          id
          householdId
          categoryId
          name
          amountYen
          actualYen
          remainingYen
          startsOn
          endsOn
          archived
          revision
        }
      }
    }
  }
`;

export const analysisRecurringExpensesFragment = graphql`
  fragment AnalysisRecurringExpensesFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    first: { type: "Int", defaultValue: 100 }
    after: { type: "String" }
  )
  @refetchable(queryName: "AnalysisRecurringExpensesPaginationQuery") {
    recurringExpenses(
      householdId: $householdId
      includeArchived: false
      first: $first
      after: $after
    )
      @connection(
        key: "AnalysisRecurringExpensesFragment_recurringExpenses"
        filters: ["householdId", "includeArchived"]
      ) {
      edges {
        node {
          id
          householdId
          name
          merchant
          amountYen
          currency
          categoryId
          paymentAccountId
          cadence
          startsOn
          nextDueOn
          endsOn
          archived
          revision
        }
      }
    }
  }
`;
