# GoSQL

## Development Workflow

Follow the TDD cycle: **List → Red → Green → Refactor**

- **List**: enumerate the tasks to work on and choose which one to tackle. Skip this step if the task is already clear.
- **Red**: write failing tests, then stop and wait for user confirmation before proceeding
- **Green**: implement the minimum code to make the tests pass
- **Refactor**: clean up the code while keeping tests green

Do NOT proceed from Red to Green without explicit user approval.

## Comments

Write all comments in English. This applies to Go doc comments, inline comments, and comments in config files such as `.github/workflows/*.yml` and `.golangci.yml`.

Commit message bodies are the exception: those may be written in Japanese.

## Commit Message

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <subject>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`. Scope is optional.

Keep the body to five lines at most. Say why the change was made, not what the
diff already shows. Longer reasoning belongs in a doc comment or a pull request
description, where it stays next to the code or the discussion it came from.

## Unit Tests

Use Table Driven Tests to group multiple cases for the same function. Each test struct element must be written across multiple lines for readability.

```go
func TestFoo(t *testing.T) {
    tests := []struct {
        name  string
        input int
        want  int
    }{
        {"returns 0 for zero input", 0, 0},
        {"returns doubled value for positive input", 3, 6},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if got := Foo(tt.input); got != tt.want {
                t.Errorf("Foo() = %d, want %d", got, tt.want)
            }
        })
    }
}
```

Test case names must describe what is being tested, so the intent is clear without reading the implementation.

## Error Handling

When returning errors, wrap them with `fmt.Errorf` and `%w` to add context about which operation failed.

```go
// Bad
return nil, err

// Good
return nil, fmt.Errorf("create directory %s: %w", dbDir, err)
```

Use `%w` (not `%v`) so callers can inspect the original error with `errors.Is` / `errors.As`.

**When to wrap:**
- Returning an error from an external call (os, io, etc.) where the caller cannot tell what operation failed without context.

**When NOT to wrap:**
- Re-returning an error that has already been wrapped to avoid double-wrapping.
- The function name and call site already make the context obvious.

```go
// Bad
{"case1", ...},
{"test A", ...},

// Good
{"returns true when filename and blkNum are equal", ...},
{"returns false when blkNum differs", ...},
```
