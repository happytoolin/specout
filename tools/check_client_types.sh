#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/specout-client-types.XXXXXX")
trap 'rm -r "$tmp"' EXIT

cd "$root"

GO_SPEC_ONLY=1 go run ./cmd/demo >"$tmp/demo.openapi.json"
GO_SPEC_ONLY=1 go run ./examples/petstore >"$tmp/petstore.openapi.json"
GO_SPEC_ONLY=1 go run ./examples/msgraph >"$tmp/msgraph.openapi.json"
for name in github stripe cloudflare; do
	go run ./examples/compatibility "$name" >"$tmp/$name.openapi.json"
done

for name in demo petstore msgraph github stripe cloudflare; do
	npx --yes -p openapi-typescript@7.13.0 openapi-typescript \
		"$tmp/$name.openapi.json" -o "$tmp/$name.d.ts"
done

cat >"$tmp/consumer.ts" <<'EOF'
import type { paths as DemoPaths } from "./demo";
import type { paths as PetstorePaths } from "./petstore";
import type { paths as MSGraphPaths } from "./msgraph";
import type { paths as GitHubPaths } from "./github";
import type { paths as StripePaths } from "./stripe";
import type {
  components as CloudflareComponents,
  paths as CloudflarePaths,
} from "./cloudflare";

type Operations = [
  NonNullable<DemoPaths["/onboarding"]["get"]>,
  NonNullable<PetstorePaths["/pet"]["post"]>,
  NonNullable<MSGraphPaths["/me"]["get"]>,
  NonNullable<GitHubPaths["/licenses"]["get"]>,
  NonNullable<StripePaths["/v1/country_specs"]["get"]>,
  NonNullable<CloudflarePaths["/user/tokens/verify"]["get"]>,
];

declare const operations: Operations;
void operations;

type CloudflareSuccess =
  CloudflareComponents["schemas"]["cloudflareVerify"]["success"];
const success: CloudflareSuccess = true;
void success;

// @ts-expect-error The upstream enum requires literal true.
const invalidSuccess: CloudflareSuccess = false;
void invalidSuccess;
EOF

npx --yes -p typescript@7.0.2 tsc \
	--noEmit \
	--strict \
	--module ESNext \
	--moduleResolution Bundler \
	--noUncheckedIndexedAccess \
	"$tmp/consumer.ts" "$tmp"/*.d.ts

echo "client type generation and compilation passed"
