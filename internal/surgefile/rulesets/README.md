These files are a snapshot of the current Surge profile's local RULE-SET
files. They are embedded into the binary and expanded inline by
`internal/surgefile/rules.go`, so generated profiles do not depend on
`~/Library/Application Support/Surge`.

Refresh the snapshot with:

```bash
./scripts/sync-surge-rules.sh
```

Then rebuild the CLI and the desktop app. The files contain only
domain/IP rules; no proxy credentials.

If you add or remove a rule-set category, also update the `defaultRuleSets`
mapping and `DefaultProxyGroups` in `rules.go` so the new policy group is
defined.
