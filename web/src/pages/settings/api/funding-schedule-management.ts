import { fetchQuery, graphql } from "react-relay";

import {
  commitGraphQLMutationResult,
  getRelayEnvironment,
  type FundingSchedulesQuery,
  type FundingSchedulesQuery$data,
  type SetFundingScheduleMutation,
} from "../../../shared/api";

const setFundingScheduleMutation = graphql`
  mutation SetFundingScheduleMutation($input: SetFundingScheduleInput!) {
    setFundingSchedule(input: $input) {
      __typename
      ... on SetFundingScheduleSuccess {
        result {
          id
          householdId
          memberId
          poolId
          amountYen
          cadence
          startsOn
          endsOn
          active
          createdAt
        }
      }
      ... on MutationError {
        code
        message
      }
    }
  }
`;

const fundingSchedulesQuery = graphql`
  query FundingSchedulesQuery($householdId: ID!, $active: Boolean, $after: String, $first: Int) {
    fundingSchedules(householdId: $householdId, active: $active, after: $after, first: $first) {
      edges {
        cursor
        node {
          id
          householdId
          memberId
          poolId
          amountYen
          cadence
          startsOn
          endsOn
          active
          createdAt
        }
      }
      pageInfo {
        endCursor
        hasNextPage
      }
    }
  }
`;

export type FundingSchedule = {
  id: string;
  memberId: string;
  poolId: string;
  amountYen: number;
  cadence: "MONTHLY" | "WEEKLY" | "CUSTOM";
  startsOn: string;
  endsOn: string | null;
  active: boolean;
};

type FundingScheduleRecord =
  FundingSchedulesQuery$data["fundingSchedules"]["edges"][number]["node"];

export async function readFundingSchedules(householdId: string): Promise<FundingSchedule[]> {
  const schedules: FundingSchedule[] = [];
  let after: string | null = null;
  for (;;) {
    const result: FundingSchedulesQuery$data | undefined = await fetchQuery<FundingSchedulesQuery>(
      getRelayEnvironment(),
      fundingSchedulesQuery,
      { householdId, active: null, first: 100, after },
      { fetchPolicy: "network-only" },
    ).toPromise();
    if (!result) throw new Error("入金予定を取得できませんでした");
    schedules.push(...result.fundingSchedules.edges.map(({ node }) => mapFundingSchedule(node)));
    const endCursor: string | null | undefined = result.fundingSchedules.pageInfo.endCursor;
    if (!result.fundingSchedules.pageInfo.hasNextPage || !endCursor || endCursor === after) {
      return schedules;
    }
    after = endCursor;
  }
}

export function setFundingSchedule(input: SetFundingScheduleMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    SetFundingScheduleMutation,
    NonNullable<SetFundingScheduleMutation["response"]["setFundingSchedule"]["result"]>
  >(
    setFundingScheduleMutation,
    { input },
    (response) => response.setFundingSchedule,
    "SetFundingScheduleSuccess",
    "入金予定を保存できませんでした",
  );
}

function mapFundingSchedule(record: FundingScheduleRecord): FundingSchedule {
  const cadence = record.cadence;
  if (cadence !== "MONTHLY" && cadence !== "WEEKLY" && cadence !== "CUSTOM") {
    throw new Error(`未対応の入金予定周期です: ${cadence}`);
  }
  return {
    id: record.id,
    memberId: record.memberId,
    poolId: record.poolId,
    amountYen: Number(record.amountYen),
    cadence,
    startsOn: record.startsOn,
    endsOn: record.endsOn ?? null,
    active: record.active,
  };
}
