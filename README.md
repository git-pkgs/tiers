# tiers

Given a set of local package checkouts, `tiers` parses their manifests, keeps the dependency edges that stay inside the set, and layers the result so each package appears in a tier above every package it depends on. Tier 0 has no in-set dependencies; tier N depends only on tiers below N. Use it to compute a release order across a group of related repositories.

Manifest parsing is delegated to [manifests](https://github.com/git-pkgs/manifests), so any ecosystem where that library reports both a package's own name and its dependency names works here as-is: currently Go modules, RubyGems, Cargo, npm, Composer, Hex, and others (see the manifests README for the full list).

## Install

Install the CLI:

```bash
go install github.com/git-pkgs/tiers/cmd/tiers@latest
```

Add the library to your Go module:

```sh
go get github.com/git-pkgs/tiers
```

## CLI

```bash
tiers [flags] <dir>...
```

| flag | |
| --- | --- |
| `-ecosystem` | ecosystem to operate on (`golang`, `gem`, `npm`, ...) |
| `-json` | emit tiers as JSON |
| `-dot` | emit the induced graph as graphviz |
| `-v` | print each package's directory and in-set dependencies |

Each run covers one ecosystem: when the input directories contain manifests for exactly one it is selected automatically; otherwise the run stops and lists the ecosystems present so you can pass `-ecosystem`. Formats that describe consumed tooling rather than a publishable package (`Dockerfile`, GitHub Actions workflows, `.gitmodules`, `.pre-commit-config.yaml`, `.tool-versions`, `Brewfile`) are always ignored.

Each argument is a directory containing a manifest at its root (a `go.mod`, a `.gemspec`, a `Cargo.toml`, and so on). A workspace file that points at nested members contributes each member as its own package. A manifest that lists dependencies but omits its own package name (a bare `Gemfile`, `requirements.txt`) becomes a package named after its directory so it still lands in the top tier as a consumer.

```console
$ tiers ~/code/git-pkgs/*/
0  github.com/git-pkgs/cooldown  github.com/git-pkgs/managers  github.com/git-pkgs/pom  github.com/git-pkgs/vers  ...
1  github.com/git-pkgs/archives  github.com/git-pkgs/purl  ...
2  github.com/git-pkgs/manifests  github.com/git-pkgs/registries  github.com/git-pkgs/resolve  ...
3  github.com/git-pkgs/enrichment  github.com/git-pkgs/pin
4  github.com/git-pkgs/brief  github.com/git-pkgs/git-pkgs  github.com/git-pkgs/proxy
```

Only direct dependencies form edges; indirect requirements in a `go.mod` are ignored on the basis that some other package in the set already holds the direct edge. A dependency cycle stops the run with an error naming the packages involved.

## Bumping

`tiers bump` walks the graph in tier order and, for each package, updates every in-set dependency that has a target version. The update is performed through [managers](https://github.com/git-pkgs/managers).

```bash
tiers bump -set github.com/git-pkgs/vers=v0.7.1 -set github.com/git-pkgs/pom=v0.1.8 ~/code/git-pkgs/*/
```

`-set name=version` may be repeated; each name must be a package in the graph. `-dry-run` prints the commands and skips execution. `-ecosystem` restricts the graph as for the list command.

An ecosystem is refused with an error, before any command runs, when the mapped manager would write the requested version somewhere other than the manifest that discovery parsed. The error names the tracking issue. RubyGems are refused because `bundle add` writes to the Gemfile, leaving the gemspec constraint unchanged ([#40](https://github.com/git-pkgs/managers/issues/40)).

## Library

```go
import "github.com/git-pkgs/tiers"

g, err := tiers.Discover("./purl", "./vers", "./manifests")
if err != nil {
    return err
}
layers, err := g.Tiers()
if err != nil {
    return err
}
for i, tier := range layers {
    for _, p := range tier {
        fmt.Printf("%d %s %s\n", i, p.Ecosystem, p.Name)
    }
}
```

`Graph.Filter(ecosystem)` returns a subgraph for one ecosystem. Package order within a tier is sorted by ecosystem then name, so repeated runs over the same inputs produce identical output.

## License

[MIT](LICENSE).
