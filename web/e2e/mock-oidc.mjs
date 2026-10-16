import { createSign, generateKeyPairSync, randomBytes, createHash } from "node:crypto";
import { createServer } from "node:http";
import { URL } from "node:url";

const issuer = "http://127.0.0.1:8090";
const clientId = "horse-e2e";
const clientSecret = "horse-e2e-only";
const keyId = "horse-e2e-key";
const { privateKey, publicKey: publicKeyObject } = generateKeyPairSync("rsa", {
  modulusLength: 2048,
});
const publicKey = publicKeyObject.export({ format: "jwk" });
const discovery = {
  issuer,
  authorization_endpoint: `${issuer}/authorize`,
  token_endpoint: `${issuer}/token`,
  jwks_uri: `${issuer}/jwks`,
  response_types_supported: ["code"],
  subject_types_supported: ["public"],
  id_token_signing_alg_values_supported: ["RS256"],
  token_endpoint_auth_methods_supported: ["client_secret_basic", "client_secret_post"],
  code_challenge_methods_supported: ["S256"],
};
const codes = new Map();

function sendJson(response, status, body) {
  response.writeHead(status, { "content-type": "application/json" });
  response.end(JSON.stringify(body));
}

function encodeBase64Url(value) {
  return Buffer.from(value).toString("base64url");
}

function signIdToken(nonce) {
  const now = Math.floor(Date.now() / 1000);
  const header = encodeBase64Url(JSON.stringify({ alg: "RS256", kid: keyId, typ: "JWT" }));
  const claims = encodeBase64Url(
    JSON.stringify({
      iss: issuer,
      sub: "horse-e2e-oidc-subject",
      aud: clientId,
      exp: now + 300,
      iat: now,
      nonce,
      name: "Horse E2E User",
      preferred_username: "horse-e2e-user",
      email: "horse-e2e@example.test",
      email_verified: true,
    }),
  );
  const signingInput = `${header}.${claims}`;
  const signer = createSign("RSA-SHA256");
  signer.update(signingInput);
  signer.end();
  return `${signingInput}.${signer.sign(privateKey).toString("base64url")}`;
}

async function readForm(request) {
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  return new URLSearchParams(Buffer.concat(chunks).toString());
}

createServer(async (request, response) => {
  const url = new URL(request.url ?? "/", issuer);
  if (request.method === "GET" && url.pathname === "/.well-known/openid-configuration") {
    sendJson(response, 200, discovery);
    return;
  }
  if (request.method === "GET" && url.pathname === "/jwks") {
    sendJson(response, 200, {
      keys: [{ ...publicKey, kid: keyId, use: "sig", alg: "RS256" }],
    });
    return;
  }
  if (request.method === "GET" && url.pathname === "/authorize") {
    const redirectUri = url.searchParams.get("redirect_uri");
    const state = url.searchParams.get("state");
    const nonce = url.searchParams.get("nonce");
    const challenge = url.searchParams.get("code_challenge");
    if (
      url.searchParams.get("client_id") !== clientId ||
      url.searchParams.get("response_type") !== "code" ||
      url.searchParams.get("code_challenge_method") !== "S256" ||
      !redirectUri ||
      !state ||
      !nonce ||
      !challenge
    ) {
      response.writeHead(400);
      response.end("invalid authorization request");
      return;
    }
    const code = randomBytes(24).toString("base64url");
    codes.set(code, { challenge, nonce, redirectUri, state });
    const callback = new URL(redirectUri);
    callback.searchParams.set("code", code);
    callback.searchParams.set("state", state);
    response.writeHead(302, { location: callback.toString() });
    response.end();
    return;
  }
  if (request.method === "POST" && url.pathname === "/token") {
    const form = await readForm(request);
    const authorization = request.headers.authorization ?? "";
    const expectedAuthorization = `Basic ${Buffer.from(`${clientId}:${clientSecret}`).toString("base64")}`;
    const clientAuthenticated =
      authorization === expectedAuthorization ||
      (form.get("client_id") === clientId && form.get("client_secret") === clientSecret);
    const code = form.get("code") ?? "";
    const issued = codes.get(code);
    const verifier = form.get("code_verifier") ?? "";
    const challenge = createHash("sha256").update(verifier).digest("base64url");
    if (
      !clientAuthenticated ||
      form.get("grant_type") !== "authorization_code" ||
      !issued ||
      form.get("redirect_uri") !== issued.redirectUri ||
      challenge !== issued.challenge
    ) {
      sendJson(response, 400, { error: "invalid_grant" });
      return;
    }
    codes.delete(code);
    sendJson(response, 200, {
      access_token: randomBytes(24).toString("base64url"),
      token_type: "Bearer",
      expires_in: 300,
      id_token: signIdToken(issued.nonce),
    });
    return;
  }
  response.writeHead(404);
  response.end();
}).listen(8090, "127.0.0.1");
