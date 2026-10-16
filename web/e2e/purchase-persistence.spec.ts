import { expect, test, type APIRequestContext } from "@playwright/test";
import { z } from "zod";

const sessionToken = "horse-e2e-session-token";
const graphqlEnvelopeSchema = z.object({
  data: z.unknown().optional(),
  errors: z.array(z.object({ message: z.string() })).optional(),
});

async function graphql<T>(
  request: APIRequestContext,
  query: string,
  dataSchema: z.ZodType<T>,
  variables: Record<string, unknown> = {},
) {
  const response = await request.post("/graphql", {
    headers: {
      origin: "http://127.0.0.1:3000",
      cookie: `horse_session=${sessionToken}`,
    },
    data: { query, variables },
  });
  expect(response.ok()).toBeTruthy();
  const payload = graphqlEnvelopeSchema.parse(await response.json());
  expect(payload.errors).toBeUndefined();
  return dataSchema.parse(payload.data);
}

test("台帳から複数品目の買い物を保存し、再読み込み後も表示する", async ({
  page,
  request,
  baseURL,
}) => {
  const pageErrors: Error[] = [];
  const failedGraphqlRequests: Array<{ operation: string; error: string }> = [];
  const cancelledSubscriptions: Array<{ operation: string; error: string }> = [];
  page.on("pageerror", (error) => pageErrors.push(error));
  page.on("requestfailed", (failedRequest) => {
    if (!failedRequest.url().endsWith("/graphql")) return;
    const query = String(failedRequest.postDataJSON()?.query ?? "");
    const failure = {
      operation: query.match(/(?:query|mutation|subscription)\s+(\w+)/)?.[1] ?? "anonymous",
      error: failedRequest.failure()?.errorText ?? "unknown",
    };
    if (failure.operation.endsWith("Subscription") && failure.error === "net::ERR_ABORTED") {
      cancelledSubscriptions.push(failure);
    } else {
      failedGraphqlRequests.push(failure);
    }
  });
  const schema = await graphql(
    request,
    "{ __schema { queryType { name } types { name fields { name } } } }",
    z.object({
      __schema: z.object({
        queryType: z.object({ name: z.string() }),
        types: z.array(
          z.object({
            name: z.string(),
            fields: z
              .array(z.object({ name: z.string() }))
              .nullable()
              .optional(),
          }),
        ),
      }),
    }),
  );
  expect(schema.__schema.queryType.name).toBe("Query");
  expect(
    schema.__schema.types
      .find((type) => type.name === "EffectiveUnitPrice")
      ?.fields?.map((field) => field.name),
  ).toContain("denominatorYen");

  await page.context().addCookies([
    {
      name: "horse_session",
      value: sessionToken,
      url: baseURL!,
      httpOnly: true,
      sameSite: "Lax",
    },
  ]);
  const householdData = await graphql(
    request,
    `
      mutation {
        createHousehold(input: { name: "E2E家計", memberNames: ["本人"] }) {
          __typename
          ... on CreateHouseholdSuccess {
            result {
              id
            }
          }
          ... on MutationError {
            message
          }
        }
      }
    `,
    z.object({
      createHousehold: z.object({
        __typename: z.string(),
        result: z.object({ id: z.string() }).optional(),
        message: z.string().optional(),
      }),
    }),
  );
  const householdId = householdData.createHousehold.result?.id;
  expect(householdId).toBeTruthy();
  const viewer = await graphql(
    request,
    "query { viewer { households { id } } }",
    z.object({ viewer: z.object({ households: z.array(z.object({ id: z.string() })) }) }),
  );
  expect(viewer.viewer.households.map((household) => household.id)).toContain(householdId);
  const poolData = await graphql(
    request,
    `
      mutation CreatePool($input: CreatePoolInput!) {
        createPool(input: $input) {
          __typename
          ... on CreatePoolSuccess {
            result {
              id
              fundingRuleVersion
            }
          }
        }
      }
    `,
    z.object({
      createPool: z.object({
        __typename: z.string(),
        result: z.object({ id: z.string(), fundingRuleVersion: z.number() }).optional(),
      }),
    }),
    { input: { householdId, name: "E2E共有", kind: "SHARED" } },
  );
  const poolId = poolData.createPool.result?.id;
  expect(poolId).toBeTruthy();
  const household = await graphql(
    request,
    `query { household(id: "${householdId}") { members { id displayName } } }`,
    z.object({
      household: z.object({
        members: z.array(z.object({ id: z.string(), displayName: z.string() })),
      }),
    }),
  );
  const actorMemberId = household.household.members.find(
    (member) => member.displayName === "E2E Tester",
  )?.id;
  expect(actorMemberId).toBeTruthy();
  const fundingRule = await graphql(
    request,
    `
      mutation SetPoolFundingRule($input: SetPoolFundingRuleInput!) {
        setPoolFundingRule(input: $input) {
          __typename
          ... on SetPoolFundingRuleUserError {
            message
          }
        }
      }
    `,
    z.object({
      setPoolFundingRule: z.object({ __typename: z.string(), message: z.string().optional() }),
    }),
    {
      input: {
        householdId,
        poolId,
        expectedVersion: poolData.createPool.result!.fundingRuleVersion,
        configuration: {
          shared: {
            fundingShares: [{ memberId: actorMemberId, weight: 1 }],
            settlementPolicy: "BALANCE_TO_STANDARD",
          },
        },
      },
    },
  );
  expect(fundingRule.setPoolFundingRule.__typename).toBe("SetPoolFundingRuleSuccess");
  const allocationData = await graphql(
    request,
    `
      mutation SetDefaultAllocation($input: SetDefaultAllocationInput!) {
        setDefaultAllocation(input: $input) {
          __typename
        }
      }
    `,
    z.object({ setDefaultAllocation: z.object({ __typename: z.string() }) }),
    { input: { householdId, allocations: [{ poolId, weight: 1 }] } },
  );
  expect(allocationData.setDefaultAllocation.__typename).toBe("SetDefaultAllocationSuccess");
  const configuredDefaults = await graphql(
    request,
    `query { household(id: "${householdId}") { defaultAllocation { poolId weight } } }`,
    z.object({
      household: z.object({
        defaultAllocation: z.array(z.object({ poolId: z.string(), weight: z.number() })),
      }),
    }),
  );
  expect(configuredDefaults.household.defaultAllocation).toEqual([{ poolId, weight: 1 }]);
  const accountData = await graphql(
    request,
    `
      mutation CreateAccount($input: CreateAccountInput!) {
        createAccount(input: $input) {
          __typename
          ... on CreateAccountSuccess {
            result {
              id
            }
          }
          ... on CreateAccountUserError {
            message
          }
        }
      }
    `,
    z.object({
      createAccount: z.object({
        __typename: z.string(),
        result: z.object({ id: z.string() }).optional(),
        message: z.string().optional(),
      }),
    }),
    { input: { householdId, name: "E2E銀行", kind: "BANK", poolId } },
  );
  expect(accountData.createAccount.result?.id).toBeTruthy();

  await page.goto(`/households/${householdId}/ledger`);
  await expect(page.getByRole("heading", { name: "帳簿" })).toBeVisible();
  await page.getByRole("button", { name: "買い物を追加" }).first().click();
  await expect(page.getByRole("heading", { name: "買い物を追加" })).toBeVisible();
  await page.getByRole("combobox", { name: "店名" }).fill("E2Eスーパー");
  await page.getByRole("combobox", { name: "決済元を検索 1" }).fill("E2E銀行");
  await page.getByRole("option", { name: "E2E銀行" }).click();
  await page.getByLabel("品名").last().fill("りんご");
  await page.getByLabel("金額").last().fill("198");
  await page.getByRole("button", { name: "品目を追加" }).click();
  await page.getByLabel("品名").last().fill("牛乳");
  await page.getByLabel("金額").last().fill("238");
  await page.getByRole("button", { name: "品目を追加" }).click();
  await expect(page.getByText(/^2品目/)).toBeVisible();
  const saveButton = page.getByRole("button", { name: "保存して送信" });
  await expect(saveButton).toBeEnabled();
  const responsePromise = page.waitForResponse(
    (response) => {
      const graphqlRequest = response.request();
      return (
        response.url().endsWith("/graphql") &&
        String(graphqlRequest.postDataJSON()?.query ?? "").includes(
          "mutation CreateAndPostPurchaseMutation",
        )
      );
    },
    { timeout: 10_000 },
  );
  await saveButton.click();
  await expect(page.getByText("送信待ちに保存しました")).toBeVisible();
  const response = await responsePromise;
  expect(response.ok()).toBeTruthy();
  const result = z
    .object({
      data: z
        .object({
          createAndPostPurchase: z
            .object({
              __typename: z.string(),
              code: z.string().optional(),
              message: z.string().optional(),
              result: z
                .object({ status: z.string(), items: z.array(z.object({ name: z.string() })) })
                .optional(),
            })
            .optional(),
        })
        .optional(),
      errors: z.array(z.object({ message: z.string() })).optional(),
    })
    .parse(await response.json());
  expect(result.errors).toBeUndefined();
  expect(result.data?.createAndPostPurchase?.__typename).toBe("CreateAndPostPurchaseSuccess");
  expect(result.data?.createAndPostPurchase?.result?.items.map((item) => item.name)).toEqual([
    "りんご",
    "牛乳",
  ]);
  await expect(page.getByRole("heading", { name: "帳簿" })).toBeVisible();
  await page.reload();
  await expect(page.getByText("E2Eスーパー").first()).toBeVisible();
  await page.getByRole("combobox", { name: "単位" }).selectOption({ label: "品目" });
  await expect(page.getByText("りんご").first()).toBeVisible();
  await expect(page.getByText("牛乳").first()).toBeVisible();
  expect(pageErrors).toEqual([]);
  expect(failedGraphqlRequests).toEqual([]);
  expect(cancelledSubscriptions.length).toBeGreaterThan(0);
  expect(
    cancelledSubscriptions.every((failure) => failure.operation === "PurchaseChangesSubscription"),
  ).toBe(true);
});
