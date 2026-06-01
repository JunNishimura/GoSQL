# GoSQL

## Commit Message

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <subject>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`. Scope is optional.

## Unit Tests

Test case names must describe what is being tested, so the intent is clear without reading the implementation.

```go
// Bad
{"case1", ...},
{"test A", ...},

// Good
{"returns true when filename and blkNum are equal", ...},
{"returns false when blkNum differs", ...},
```
