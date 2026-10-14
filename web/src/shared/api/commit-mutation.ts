import type { GraphQLTaggedNode, MutationParameters } from "relay-runtime";

import { commitMutation as commitRelayMutation } from "react-relay";

import { getRelayEnvironment } from "./household";

type MutationResponse = {
  __typename: string;
  message?: string | null;
};

export function commitGraphQLMutation<TOperation extends MutationParameters, TResult>(
  mutation: GraphQLTaggedNode,
  variables: TOperation["variables"],
  readResult: (response: TOperation["response"]) => TResult,
): Promise<TResult> {
  return new Promise((resolve, reject) =>
    commitRelayMutation<TOperation>(getRelayEnvironment(), {
      mutation,
      variables,
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }
        try {
          resolve(readResult(response));
        } catch (reason) {
          reject(reason);
        }
      },
      onError: reject,
    }),
  );
}

export function commitGraphQLMutationSuccess<TOperation extends MutationParameters>(
  mutation: GraphQLTaggedNode,
  variables: TOperation["variables"],
  readMutation: (response: TOperation["response"]) => MutationResponse,
  successType: string,
  failureMessage: string,
): Promise<void> {
  return commitGraphQLMutation<TOperation, void>(mutation, variables, (response) => {
    assertGraphQLMutationSuccess(
      readMutation(response),
      successType,
      failureMessage,
      failureMessage,
    );
  });
}

export function assertGraphQLMutationSuccess(
  response: MutationResponse,
  successType: string,
  failureMessage: string,
  unknownTypeMessage: string | ((typename: string) => string) = "未対応のMutation結果です",
): void {
  if (response.__typename === successType) return;
  if (response.__typename === "MutationError" || response.__typename.endsWith("UserError")) {
    throw new Error(response.message ?? failureMessage);
  }
  throw new Error(
    typeof unknownTypeMessage === "string"
      ? `${unknownTypeMessage}: ${response.__typename}`
      : unknownTypeMessage(response.__typename),
  );
}

export function readGraphQLMutationResult<TResult>(
  response: MutationResponse & { result?: TResult | null },
  successType: string,
  failureMessage: string,
  unknownTypeMessage: string | ((typename: string) => string) = "未対応のMutation結果です",
): TResult {
  assertGraphQLMutationSuccess(response, successType, failureMessage, unknownTypeMessage);
  if (response.result == null) throw new Error(failureMessage);
  return response.result;
}

export function commitGraphQLMutationResult<TOperation extends MutationParameters, TResult>(
  mutation: GraphQLTaggedNode,
  variables: TOperation["variables"],
  readMutation: (
    response: TOperation["response"],
  ) => MutationResponse & { result?: TResult | null },
  successType: string,
  failureMessage: string,
): Promise<TResult> {
  return commitGraphQLMutation<TOperation, TResult>(mutation, variables, (response) =>
    readGraphQLMutationResult(readMutation(response), successType, failureMessage),
  );
}
