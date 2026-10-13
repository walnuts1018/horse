import { commitMutation, fetchQuery } from "react-relay";
import {
  ApiError,
  getRelayEnvironment,
  MutationUserError,
  type Allocation,
  type Household,
  type HouseholdQuery,
  type HouseholdQuery$data,
  type HouseholdsQuery,
  type CreateHouseholdMutation,
  type CreateHouseholdMutation$variables,
  type AcceptHouseholdInvitationMutation,
  type AcceptHouseholdInvitationMutation$variables,
} from "../../../shared/api";
import {
  acceptHouseholdInvitationMutation,
  createHouseholdMutation,
  householdQuery,
  householdsQuery,
} from "./operations";

type HouseholdRecord = NonNullable<HouseholdQuery$data["household"]>;
type PoolRecord = HouseholdRecord["pools"][number];

export async function readHouseholds(): Promise<Array<{ id: string; name: string }>> {
  const data = await fetchQuery<HouseholdsQuery>(
    getRelayEnvironment(),
    householdsQuery,
    {},
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data) throw new Error("家計簿一覧を取得できませんでした");
  return data.households.map(({ id, name }) => ({ id, name }));
}

export async function readHousehold(id: string): Promise<Household> {
  const data = await fetchQuery<HouseholdQuery>(
    getRelayEnvironment(),
    householdQuery,
    { id },
    { fetchPolicy: "network-only" },
  ).toPromise();
  if (!data?.household) throw new Error("家計簿を取得できませんでした");
  return mapHousehold(data.household);
}

export function createHousehold(
  input: CreateHouseholdMutation$variables["input"],
): Promise<{ id: string }> {
  return new Promise((resolve, reject) =>
    commitMutation<CreateHouseholdMutation>(getRelayEnvironment(), {
      mutation: createHouseholdMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new ApiError(errors[0].message));
          return;
        }
        const result = response.createHousehold;
        switch (result.__typename) {
          case "CreateHouseholdSuccess":
            if (!result.result) {
              reject(new Error("家計簿を作成できませんでした"));
              return;
            }
            resolve({ id: result.result.id });
            return;
          case "MutationError":
            reject(
              new MutationUserError(result.message ?? "家計簿を作成できませんでした", result.code),
            );
            return;
          default:
            reject(new Error(`未対応の家計簿作成結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

export function acceptHouseholdInvitation(
  input: AcceptHouseholdInvitationMutation$variables["input"],
): Promise<{ id: string; name: string }> {
  return new Promise((resolve, reject) =>
    commitMutation<AcceptHouseholdInvitationMutation>(getRelayEnvironment(), {
      mutation: acceptHouseholdInvitationMutation,
      variables: { input },
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new ApiError(errors[0].message));
          return;
        }
        const result = response.acceptHouseholdInvitation;
        switch (result.__typename) {
          case "AcceptHouseholdInvitationSuccess":
            if (!result.result) {
              reject(new Error("家計簿への参加を完了できませんでした"));
              return;
            }
            resolve({ id: result.result.id, name: result.result.name });
            return;
          case "MutationError":
            reject(
              new MutationUserError(
                result.message ?? "家計簿への参加を完了できませんでした",
                result.code,
              ),
            );
            return;
          default:
            reject(new Error(`未対応の招待受け入れ結果です: ${result.__typename}`));
        }
      },
      onError: reject,
    }),
  );
}

function mapHousehold(record: HouseholdRecord): Household {
  const pools = record.pools.map(mapPool);
  const poolNames = new Map(pools.map((pool) => [pool.id, pool.name]));
  const defaultAllocation: Allocation[] = record.defaultAllocation.map((allocation) => {
    const poolName = poolNames.get(allocation.poolId);
    if (!poolName) throw new Error(`家計簿の負担先が見つかりません: ${allocation.poolId}`);
    return {
      poolId: allocation.poolId,
      poolName,
      weight: mapWeight(allocation.weight),
    };
  });

  return {
    id: record.id,
    name: record.name,
    revision: record.revision,
    members: record.members.map((member) => ({
      id: member.id,
      displayName: member.displayName,
      role: mapMemberRole(member.role),
    })),
    pools,
    accounts: record.accounts.map((account) => ({
      id: account.id,
      name: account.name,
      kind: account.kind,
      poolId: account.poolId,
    })),
    categories: record.categories.map((category) => ({
      id: category.id,
      name: category.name,
      parentId: category.parentId,
      archived: category.archived,
    })),
    defaultAllocation,
  };
}

function mapMemberRole(role: string): Household["members"][number]["role"] {
  switch (role) {
    case "OWNER":
    case "MEMBER":
      return role;
    default:
      throw new Error(`未対応のメンバーroleです: ${role}`);
  }
}

function mapPool(record: PoolRecord): Household["pools"][number] {
  const fields = {
    id: record.id,
    name: record.name,
    defaultShareWeight: mapWeight(record.defaultShareWeight),
    fundingShares: record.fundingShares.map((share) => ({
      memberId: share.memberId,
      memberName: share.memberName,
      weight: mapWeight(share.weight),
    })),
    fundingRuleVersion: record.fundingRuleVersion,
  };
  switch (record.__typename) {
    case "PersonalPool":
      if (!record.ownerMemberId) throw new Error(`個人poolに所有者がありません: ${record.id}`);
      return { ...fields, __typename: "PersonalPool", ownerMemberId: record.ownerMemberId };
    case "SharedPool":
      if (
        record.settlementPolicy !== "BALANCE_TO_STANDARD" &&
        record.settlementPolicy !== "REIMBURSE_PERSONAL"
      ) {
        throw new Error(`未対応の精算方式です: ${record.id}`);
      }
      return {
        ...fields,
        __typename: "SharedPool",
        settlementPolicy: record.settlementPolicy,
      };
    case "UnconfiguredPersonalPool":
      return { ...fields, __typename: "UnconfiguredPersonalPool" };
    case "UnconfiguredSharedPool":
      return { ...fields, __typename: "UnconfiguredSharedPool" };
    default:
      throw new Error(`未対応のpool型です: ${record.__typename}`);
  }
}

function mapWeight(value: unknown): number {
  if (typeof value !== "number" && typeof value !== "string") {
    throw new Error("家計簿の負担割合が不正です");
  }
  if (typeof value === "string" && value.trim() === "") {
    throw new Error("家計簿の負担割合が不正です");
  }
  const weight = Number(value);
  if (!Number.isFinite(weight)) throw new Error("家計簿の負担割合が不正です");
  return weight;
}
