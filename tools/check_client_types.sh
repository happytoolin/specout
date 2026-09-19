#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/specout-client-types.XXXXXX")
trap 'rm -r "$tmp"' EXIT

cd "$root"

GO_SPEC_ONLY=1 go run ./examples/onboarding >"$tmp/onboarding.openapi.json"
GO_SPEC_ONLY=1 go run ./examples/petstore >"$tmp/petstore.openapi.json"
GO_SPEC_ONLY=1 go run ./examples/msgraph >"$tmp/msgraph.openapi.json"

for name in onboarding petstore msgraph; do
	npx --yes -p openapi-typescript@7.13.0 openapi-typescript \
		"$tmp/$name.openapi.json" -o "$tmp/$name.d.ts"
done

cat >"$tmp/consumer.ts" <<'EOF'
import type { paths as OnboardingPaths } from "./onboarding";
import type { paths as PetstorePaths } from "./petstore";
import type { paths as MSGraphPaths } from "./msgraph";

type Operations = [
  NonNullable<OnboardingPaths["/onboarding"]["get"]>,
  NonNullable<PetstorePaths["/pet"]["post"]>,
  NonNullable<MSGraphPaths["/me"]["get"]>,
];

declare const operations: Operations;
void operations;

EOF

npx --yes -p typescript@7.0.2 tsc \
	--noEmit \
	--strict \
	--module ESNext \
	--moduleResolution Bundler \
	--noUncheckedIndexedAccess \
	"$tmp/consumer.ts" "$tmp"/*.d.ts

echo "client type generation and compilation passed"
