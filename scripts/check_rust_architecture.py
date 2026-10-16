#!/usr/bin/env python3
"""Rust workspaceのDomain/Application依存境界を検査します。"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
FORBIDDEN_EXTERNAL_LAYERS = {
    "async-graphql": "GraphQL",
    "async-graphql-axum": "GraphQL",
    "diesel": "database",
    "diesel-async": "database",
    "postgres": "database",
    "sea-orm": "database",
    "sqlx": "database",
    "tokio-postgres": "database",
}


def package_layer(package: dict[str, object]) -> str | None:
    if package["name"] == "horse-postgres-common":
        return "postgres"
    manifest_path = Path(str(package["manifest_path"]))
    relative_path = manifest_path.relative_to(ROOT)
    parts = relative_path.parts

    if parts[0] == "adapters":
        return "adapter"
    if len(parts) >= 3 and parts[0] == "contexts" and parts[2] == "postgres":
        return "postgres"
    return None


def main() -> int:
    result = subprocess.run(
        ["cargo", "metadata", "--no-deps", "--format-version", "1"],
        cwd=ROOT,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        sys.stderr.write(result.stderr)
        return result.returncode

    metadata = json.loads(result.stdout)
    packages = metadata["packages"]
    workspace_names = {package["name"] for package in packages}
    layers = {package["name"]: package_layer(package) for package in packages}
    violations: list[tuple[str, str, str]] = []

    for package in packages:
        relative_path = Path(package["manifest_path"]).relative_to(ROOT)
        parts = relative_path.parts
        if len(parts) < 3 or parts[0] != "contexts" or parts[2] not in {
            "domain",
            "application",
        }:
            continue

        for dependency in package["dependencies"]:
            dependency_name = dependency["name"]
            if dependency_name in FORBIDDEN_EXTERNAL_LAYERS:
                target_layer = FORBIDDEN_EXTERNAL_LAYERS[dependency_name]
            elif dependency.get("source") is None and dependency_name in workspace_names:
                target_layer = layers[dependency_name]
            else:
                continue
            if target_layer in {"adapter", "postgres", "database"}:
                violations.append((package["name"], dependency_name, target_layer))

    if violations:
        for source, target, layer in sorted(violations):
            print(f"{source} depends on forbidden {layer} crate {target}", file=sys.stderr)
        return 1

    print("Rust domain/application dependency boundaries are valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
