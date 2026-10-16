import { expect, test } from "@playwright/test";

test("OIDCコールバックでセッションを作成し、認証APIを利用できる", async ({ page, baseURL }) => {
  await page.goto("/auth/login?returnTo=%2F");
  await expect(page).toHaveURL(`${baseURL}/`);

  const cookies = await page.context().cookies(baseURL);
  const session = cookies.find((cookie) => cookie.name === "horse_session");
  expect(session?.httpOnly).toBe(true);
  expect(session?.sameSite).toBe("Lax");
  expect(cookies.some((cookie) => cookie.name === "horse_oidc_state")).toBe(false);

  const viewerResponse = await page.evaluate(async () => {
    const response = await fetch("/graphql", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ query: "query { viewer { subject displayName } }" }),
    });
    return { status: response.status, body: await response.json() };
  });
  expect(viewerResponse.status).toBe(200);
  expect(viewerResponse.body).toMatchObject({
    data: {
      viewer: {
        subject: "horse-e2e-oidc-subject",
        displayName: "Horse E2E User",
      },
    },
  });
});
