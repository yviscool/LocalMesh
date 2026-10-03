# Contributing

## Change process

1. Read `AGENTS.md` and the relevant documents in `docs/`.
2. Define the behavior and failure semantics before coding.
3. Write a focused failing test for new domain behavior, then implement the smallest passing change.
4. Run `make check` and review the diff with `git diff --check`.
5. Use a focused commit and open a pull request through `gh pr create`.

## Commit format

Use Conventional Commit style:

```text
<type>(<scope>): <imperative summary>
```

Allowed types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`, `build`.

## Review requirements

Reviewers should verify domain boundaries, authorization, error semantics, test coverage, observability, and Windows privilege boundaries. A passing test suite does not replace a security review for network or authentication changes.

