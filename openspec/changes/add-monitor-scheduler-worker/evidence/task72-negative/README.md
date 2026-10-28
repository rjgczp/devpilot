# Task7.2 first expanded matrix failure (2026-10-27)

The first 12-cell development matrix passed on source inventory a9b7dcf872a042f78d43e7b4303152fc15ee9a2c81b54de94e5352ba136d42e7. The expanded 16-cell matrix stopped at its first normal TestResultCommitValidationPG, exit1, source inventory 1be99bf70a1eab9659daf120ddac782c339d759c7d799044ad04f27541b512b6, dirty parent c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c. This is not a final implementation SHA acceptance.

The new cross-Workspace setup called monitorFixture twice with the same t.Name(). membersFixture derives account email from that name. Actual PG log reports users_email_key duplicate for testresultcommitvalidationpg-o@example.com. The failure occurred before the newly added mixed-scope call/ACL checks; it is not an observed permission bypass. Real isolated HTTPS/independent physical release had completed; the Run had no HTTP result persisted. No failure or success was fabricated.

Minimal repair only wraps the second setup in a named subtest so the existing helper generates distinct accounts. Existing helper, SQL, grants, network controls and assertions remain unchanged. Original PG error, receiver/physical proof and ledger are retained byte-for-byte after gzip decompression; see manifest. Passing subsequent tests must be recorded separately.
