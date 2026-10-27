# Contributing

Thanks for helping. Issues and pull requests are welcome.

- **Bugs and ideas**: open an issue with what you did, what you expected
  and what the deck showed. A screenshot or `herdr-deck --version` helps.
- **Changes to the deck as a whole** are best sent upstream to
  [FredricW/herdr-deck](https://github.com/FredricW/herdr-deck) too, so
  both stay close. Changes to what this fork adds (the herdr-projects 1.x
  board, the Linear, GitHub and Review views) belong here.

## Working on the code

Needs Go 1.27 or newer.

```sh
make build                # → bin/herdr-deck
./bin/herdr-deck --fake   # sample data, never touches herdr
make test                 # go test ./...
make lint                 # golangci-lint if installed, else gofmt + go vet
```

- Read [docs/PLAN.md](docs/PLAN.md) first, and update it when a decision
  changes. [AGENTS.md](AGENTS.md) has the short rules.
- Data code lives in `internal/source/*` and never imports the UI.
- The deck only reads herdr-projects: it never writes under
  `~/.herdr-projects`.
- UI tests compare against `internal/ui/testdata/*.golden`. After an
  intended change to the rendering, rewrite them with
  `go test ./internal/ui -update` and check the diff.
- Before a pull request: `gofmt -l .` prints nothing, and `go vet ./...`
  and `go test ./...` pass.
- A user-facing change adds a line under `## [Unreleased]` in
  [CHANGELOG.md](CHANGELOG.md), written for people who use the deck.

Keep pull requests small and about one thing. Use made-up names in tests
and examples (`acme/webapp`, `ENG-123`), never real companies, people,
tokens or paths from your own machine.
