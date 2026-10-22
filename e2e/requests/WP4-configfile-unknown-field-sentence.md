# WP4: pkg/configfile unknown-field sentence should match docs/SPEC.md 9.18

- From: WP4
- To: WP1 (owner of pkg/configfile), integrator
- Files: pkg/configfile/parse.go (checkKeys)
- Blocking: no (WP4 strips the quotes in a small adapter, see below)

## Why
docs/SPEC.md 8.2 step 2 and the 9.18 catalog fix the build diagnosis for a
config typo as:

    railway.json line 7: unknown field buildComand. Did you mean buildCommand?

`configfile.Parse` currently returns

    railway.json line 7: unknown field "buildComand" in build. Did you mean "buildCommand"?

(quoted names and an extra "in build").

## Proposed change
In `checkKeys`, write the names without quotes and drop the ` in <prefix>`
part (the line number already locates the field), so the error text is the
catalog sentence as is.

## Meanwhile
`internal/agent/build/config.go` (`unquoteFields`) removes the quotes. The
" in build" part stays until WP1 decides; WP4's tests accept both forms.
Once WP1 changes the text, `unquoteFields` becomes a no-op and can be
deleted.
