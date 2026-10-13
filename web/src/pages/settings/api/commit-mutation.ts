import { commitMutation } from "react-relay";
import type { GraphQLTaggedNode, MutationParameters } from "relay-runtime";
import { getRelayEnvironment } from "../../../shared/api";

type MutationResult<TResult> = {
  __typename: string;
  message?: string | null;
  result?: TResult | null;
};

export function commitSettingsMutation<TOperation extends MutationParameters, TResult>(
  mutation: GraphQLTaggedNode,
  variables: TOperation["variables"],
  readResult: (response: TOperation["response"]) => MutationResult<TResult>,
  successType: string,
  failureMessage: string,
): Promise<TResult> {
  return new Promise((resolve, reject) =>
    commitMutation<TOperation>(getRelayEnvironment(), {
      mutation,
      variables,
      onCompleted: (response, errors) => {
        if (errors?.length) {
          reject(new Error(errors[0].message));
          return;
        }

        const result = readResult(response);
        if (result.__typename === successType) {
          if (result.result == null) {
            reject(new Error(failureMessage));
            return;
          }
          resolve(result.result);
          return;
        }
        if (result.__typename === "MutationError") {
          reject(new Error(result.message ?? failureMessage));
          return;
        }
        reject(new Error(`未対応のMutation結果です: ${result.__typename}`));
      },
      onError: reject,
    }),
  );
}
