# AGENTS.md

## Setup commands

- Run `make help` to learn about Makefile targets
- Always prefer Makefile targets over raw `go` commands: use `make test-unit` not `go test ./pkg/...`
- To run controller tests, use `make test-controllers`

## Dependencies

- Always prefer standard library over third party packages.
- Look first in the kubernetes libraries or kubernetes ecosystem libraries (e.g. sigs.k8s.io/...) before
  pulling generic third party libraries.
- Always minimize external dependencies. If the dependency is small *and* well understood/common,
  err towards reimplementing leveraging standard library or kubernetes or kubernetes-adjacent libraries

## Development patterns

- Plan the tasks and ask clarifications to the user before to perform non mechanical change.
- Never make assumptions, rather stop and ask for confirmation or clarification.
- Only do minimal changes to fulfill the task at hand.
- Inspect the project codebase and make the change as similar as possible to existing code.
- Adopt as much as possible the patterns and idioms in the existing codebase.
- Reuse existing packages in the pkg and internal trees as much as possible.
- Focus on the task at hand and don't change anything beside this. 
- When a tool-specific guideline contradicts the generic guidelines presented in the rest of the document, always prefer the tool-specific guideline.
- Comments should provide meaningful insights into the code. Avoid filler comments that simply describe the next step, as they create unnecessary clutter, same goes for docstrings.
