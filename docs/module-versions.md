# Module versions and compatibility

Every module in a build has a version, taken from the Go module it comes from. Hydrogen reports them, and a module can
state which core versions it works with. Choosing modules (`hydrogen.yaml`, `hydrogen add`) is in
[modules](modules.md).

## Where versions come from

There is no version registry. The kernel reads the binary's build information (`runtime/debug.ReadBuildInfo`) and
matches each module's Go package to the longest Go module path that contains it:

| The module's package is in | Version shown |
|---|---|
| a dependency (`go.mod` requires it) | the required version, e.g. `v0.5.1` |
| the project's own module (including a module defined in `package main`) | `devel` |
| a dependency with a local `replace` | `devel (<replace path>)`, or the replace's version when it has one |
| the core, built from a checkout of the core | `devel` |
| no match, no build information, or `go run` of loose files (`command-line-arguments`) | `unknown` |

Pseudo-versions (`v0.0.0-2026…`) show as `devel`. First-party modules in the core module (`pages`, `images`, …)
share the core's version; `sqlite` and `postgres` are separate Go modules with their own.

Pin a version with `go get`:

```sh
go get github.com/acme/hydrogen-stripe@v1.2.0
```

## See what a build contains

The `modules` command lists the registered modules in registration order (required modules first):

```
$ ./bin/app modules
NAME      VERSION   MODULE
sqlite    v0.5.0    github.com/hydrogencms/hydrogen/modules/sqlite
images    v0.5.1    github.com/hydrogencms/hydrogen
project   devel     example.com/acme
```

The command runs before the database is opened, so it works even when the app can't start. If a module's
`RequiresCore` range does not match the core, it still prints the table and adds a `warning:` line on stderr;
every other command refuses to start.

In Go, `(*kernel.App).Modules()` returns `[]kernel.ModuleInfo{Name, Package, Module, Version, Requires}` (it
returned names before; `ModuleNames()` returns those) and `(*kernel.App).CoreVersion()` the core's version.
`kernel.DescribeModules` does the same without an `App`.

## GET /api/manage/v1/system

Needs the `system.read` policy, which the Administrator role template holds (it grants every policy, so existing
databases need no change). Other roles can be given `system.read` in the usual way.

```json
{
  "core": {"version": "v0.5.1", "go": "go1.26.1"},
  "modules": [
    {"name": "sqlite", "package": "github.com/hydrogencms/hydrogen/modules/sqlite",
     "module": "github.com/hydrogencms/hydrogen/modules/sqlite", "version": "v0.5.0", "requires": []}
  ],
  "database": "sqlite",
  "search": "database",
  "media": false
}
```

`database` is the adapter (`sqlite` or `postgres`), `search` the engine that answers searches (`database` unless a
module such as Meilisearch registered one), and `media` whether uploads are enabled. The operation is in
`/api/manage/v1/openapi.json` as `getSystem`.

## Declaring the core versions a module needs

A module that depends on a particular range of the core implements `kernel.CoreRequirer`:

```go
func (Module) RequiresCore() string { return ">=0.4.0, <0.6.0" }
```

A range is one or more comparisons joined by commas; all must hold. Operators are `=`, `!=`, `<`, `<=`, `>` and
`>=` (a bare version means `=`), versions are `MAJOR.MINOR.PATCH` with an optional `-pre.release` and an optional
`v` prefix. Build metadata (`+x.y`) is validated and ignored. Ordering follows semver precedence for every
version, pre-releases and pseudo-versions (`v0.6.0-0.2026…`) included: `0.6.0-rc.1` is below `0.6.0`, so
`<0.6.0` accepts it and `>=0.6.0` does not.

The kernel checks every module's range against the core version before it registers anything, so `serve`,
`migrate` and `config:check` all fail the same way:

```
module stripe (github.com/acme/hydrogen-stripe v1.2.0) requires hydrogen core >=0.6.0, but this build has v0.5.1;
use a version of the module that matches this core, or update the core (HYDROGEN_SKIP_COMPAT=1 skips this check)
```

The check does not run when the core version is `devel` or `unknown` (a local build or a checkout used through a
`replace`), or when `HYDROGEN_SKIP_COMPAT=1` is set. An invalid range is always an error, so a typo shows up in
development.
