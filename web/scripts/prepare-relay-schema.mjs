import { copyFileSync, mkdirSync } from "node:fs";
import { dirname } from "node:path";

const schemaPath = new URL("../.relay/schema.graphql", import.meta.url);
mkdirSync(dirname(schemaPath.pathname), { recursive: true });
copyFileSync(new URL("../../graphql/schema.graphqls", import.meta.url), schemaPath);
