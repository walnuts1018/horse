import { mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import { buildASTSchema, concatAST, parse, printSchema, validateSchema } from "graphql";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const schemaDirectory = join(scriptDirectory, "../../graphql");
const schemaPath = join(scriptDirectory, "../.relay/schema.graphql");
const documents = readdirSync(schemaDirectory)
  .filter((filename) => filename.endsWith(".graphql"))
  .toSorted()
  .map((filename) => parse(readFileSync(join(schemaDirectory, filename), "utf8")));
const schema = buildASTSchema(concatAST(documents));
const errors = validateSchema(schema);
if (errors.length > 0) {
  throw new Error(`Invalid GraphQL schema:\n${errors.map((error) => error.message).join("\n")}`);
}

mkdirSync(dirname(schemaPath), { recursive: true });
writeFileSync(schemaPath, `${printSchema(schema)}\n`);
