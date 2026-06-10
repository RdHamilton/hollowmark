.PHONY: gen-taxonomy check-taxonomy-stale

# gen-taxonomy regenerates both the Go and TypeScript analytics taxonomy files
# from services/bff/internal/analytics/taxonomy.yml.
# Run this after editing taxonomy.yml, then commit the generated files.
gen-taxonomy:
	go run scripts/gen-analytics-taxonomy.go
	npx tsx scripts/gen-analytics-taxonomy.ts

# check-taxonomy-stale verifies that the checked-in generated files are
# up-to-date with the YAML source.  Exits non-zero if they diverge.
# Used by CI and TestTaxonomyNotStale.
check-taxonomy-stale:
	@echo "Checking Go taxonomy..."
	@go run scripts/gen-analytics-taxonomy.go --stdout | diff - services/bff/internal/analytics/taxonomy.gen.go || (echo "taxonomy.gen.go is stale — run: make gen-taxonomy" && exit 1)
	@echo "Checking TypeScript taxonomy..."
	@npx tsx scripts/gen-analytics-taxonomy.ts --stdout | diff - frontend/src/services/analytics-taxonomy.gen.ts || (echo "analytics-taxonomy.gen.ts is stale — run: make gen-taxonomy" && exit 1)
	@echo "Taxonomy files are up-to-date."
