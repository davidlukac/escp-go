# Rewrites the compatibility table in README.md from safety/compatibility.json (the firmware
# allow-list reads the same file); a test fails when the README is stale.
compat-table:
	go test ./safety -run TestReadmeCompatibilityTable -update-readme

.PHONY: compat-table
