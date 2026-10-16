import { createServer } from "node:http";

const issuer = "http://127.0.0.1:8090";
const discovery = {
  issuer,
  authorization_endpoint: `${issuer}/authorize`,
  token_endpoint: `${issuer}/token`,
  jwks_uri: `${issuer}/jwks`,
  response_types_supported: ["code"],
  subject_types_supported: ["public"],
  id_token_signing_alg_values_supported: ["RS256"],
};

createServer((request, response) => {
  if (request.url === "/.well-known/openid-configuration") {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify(discovery));
    return;
  }
  if (request.url === "/jwks") {
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({ keys: [] }));
    return;
  }
  response.writeHead(404);
  response.end();
}).listen(8090, "127.0.0.1");
