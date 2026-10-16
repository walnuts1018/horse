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
  query AnalysisDashboardQuery($householdId: ID!, $from: YearMonth!, $to: YearMonth!) {
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
      dimensionId
      name
      period
      amountYen
      count
    }
    budgetPeriods(householdId: $householdId, from: $from, to: $to) {
      period
      budgets {
        budgetId
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
    ...AnalysisRecurringExpensesFragment @arguments(householdId: $householdId)
  }
`;

export const analysisEvidenceQuery = graphql`
  query AnalysisEvidenceQuery(
    $householdId: ID!
    $from: YearMonth!
    $to: YearMonth!
    $purchaseFilter: PurchaseFilter!
  ) {
    ...AnalysisUnbackedPurchasesFragment @arguments(filter: $purchaseFilter)
    ...AnalysisUnmatchedObservationsFragment
      @arguments(householdId: $householdId, from: $from, to: $to)
    ...AnalysisUndatedUnmatchedObservationsFragment @arguments(householdId: $householdId)
  }
`;

export const analysisUnbackedPurchasesFragment = graphql`
  fragment AnalysisUnbackedPurchasesFragment on Query
  @argumentDefinitions(
    filter: { type: "PurchaseFilter!" }
    first: { type: "Int", defaultValue: 8 }
    after: { type: "String" }
  )
  @refetchable(queryName: "AnalysisUnbackedPurchasesPaginationQuery") {
    purchases(filter: $filter, first: $first, after: $after)
      @connection(key: "AnalysisUnbackedPurchasesFragment_purchases", filters: ["filter"]) {
      edges {
        node {
          id
          occurredAt
          merchant
          totalYen
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
      totalCount
    }
  }
`;

export const analysisUnmatchedObservationsFragment = graphql`
  fragment AnalysisUnmatchedObservationsFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    from: { type: "YearMonth!" }
    to: { type: "YearMonth!" }
    first: { type: "Int", defaultValue: 8 }
    after: { type: "String" }
  )
  @refetchable(queryName: "AnalysisUnmatchedObservationsPaginationQuery") {
    unmatchedObservations(
      householdId: $householdId
      from: $from
      to: $to
      state: UNMATCHED
      first: $first
      after: $after
    )
      @connection(
        key: "AnalysisUnmatchedObservationsFragment_unmatchedObservations"
        filters: ["householdId", "from", "to"]
      ) {
      edges {
        node {
          __typename
          id
          ... on UnmatchedObservation {
            sourceKind
            merchant
            occurredAt
            amountYen
            orderId
            transactionId
          }
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
      totalCount
    }
  }
`;

export const analysisUndatedUnmatchedObservationsFragment = graphql`
  fragment AnalysisUndatedUnmatchedObservationsFragment on Query
  @argumentDefinitions(
    householdId: { type: "ID!" }
    first: { type: "Int", defaultValue: 8 }
    after: { type: "String" }
  )
  @refetchable(queryName: "AnalysisUndatedUnmatchedObservationsPaginationQuery") {
    undatedUnmatchedObservations(householdId: $householdId, first: $first, after: $after)
      @connection(
        key: "AnalysisUndatedUnmatchedObservationsFragment_undatedUnmatchedObservations"
        filters: ["householdId"]
      ) {
      edges {
        node {
          __typename
          id
          ... on UnmatchedObservation {
            sourceKind
            merchant
            amountYen
            orderId
            transactionId
          }
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
      totalCount
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
