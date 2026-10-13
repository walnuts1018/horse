import { readGraphql } from "../../../shared/api";
import {
  createAccountMutation,
  createPoolMutation,
  setPoolFundingRuleMutation,
} from "./operations";

export function createAccount(input: Record<string, unknown>) {
  return readGraphql(createAccountMutation, { input });
}

export function createPool(input: Record<string, unknown>) {
  return readGraphql(createPoolMutation, { input });
}

export function setPoolFundingRule(input: Record<string, unknown>) {
  return readGraphql(setPoolFundingRuleMutation, { input });
}
