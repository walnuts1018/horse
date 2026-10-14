import type {
  CreateAccountMutation,
  CreatePoolMutation,
  SetPoolFundingRuleMutation,
} from "../../../shared/api";

import { commitGraphQLMutationResult } from "../../../shared/api";
import {
  createAccountMutation,
  createPoolMutation,
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
