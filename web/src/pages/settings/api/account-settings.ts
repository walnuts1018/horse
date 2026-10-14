import type {
  CreateAccountMutation,
  CreatePoolMutation,
  SetPoolFundingRuleMutation,
  SetAccountCardLast4Mutation,
} from "../../../shared/api";

import { commitGraphQLMutationResult } from "../../../shared/api";
import {
  createAccountMutation,
  createPoolMutation,
  setAccountCardLast4Mutation,
  setPoolFundingRuleMutation,
} from "./operations";

export type PoolKind = CreatePoolMutation["variables"]["input"]["kind"];
export type AccountKind = CreateAccountMutation["variables"]["input"]["kind"];

export function createAccount(input: CreateAccountMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    CreateAccountMutation,
    NonNullable<CreateAccountMutation["response"]["createAccount"]["result"]>
  >(
    createAccountMutation,
    { input },
    (response) => response.createAccount,
    "CreateAccountSuccess",
    "口座を作成できませんでした",
  );
}

export function setAccountCardLast4(input: SetAccountCardLast4Mutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    SetAccountCardLast4Mutation,
    NonNullable<SetAccountCardLast4Mutation["response"]["setAccountCardLast4"]["result"]>
  >(
    setAccountCardLast4Mutation,
    { input },
    (response) => response.setAccountCardLast4,
    "SetAccountCardLast4Success",
    "カード末尾4桁を保存できませんでした",
  );
}

export function createPool(input: CreatePoolMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    CreatePoolMutation,
    NonNullable<CreatePoolMutation["response"]["createPool"]["result"]>
  >(
    createPoolMutation,
    { input },
    (response) => response.createPool,
    "CreatePoolSuccess",
    "プールを作成できませんでした",
  );
}

export function setPoolFundingRule(input: SetPoolFundingRuleMutation["variables"]["input"]) {
  return commitGraphQLMutationResult<
    SetPoolFundingRuleMutation,
    NonNullable<SetPoolFundingRuleMutation["response"]["setPoolFundingRule"]["result"]>
  >(
    setPoolFundingRuleMutation,
    { input },
    (response) => response.setPoolFundingRule,
    "SetPoolFundingRuleSuccess",
    "プールの負担ルールを保存できませんでした",
  );
}
