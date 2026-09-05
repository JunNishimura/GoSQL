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

### Where the intent goes

Name the test function after the unit under test and nothing else:
`TestSchemaAddField`, not `TestSchemaAddFieldKeepsTheCharacterLimit`. What a
case checks belongs in the case name, so the function name has no work to do
beyond saying which function is being tested.

Run every case inside `t.Run`, including a test that has only one. The subtest
name is where the intent lives, so a test with a single case still needs one
rather than spelling the intent out in the function name.

### Case names

Write a case name as **given / when / then**: the condition it starts from, the
call it makes, and what that has to produce. A reader should be able to tell
what a failing case was checking without opening the body.

```
given a schema with an int field, when a varchar field of the same name is added, then it reports ErrDuplicateField
```

Drop `given` when there is no condition to state — a pure function of its
arguments starts from nothing.

```
when a string of 10 characters is measured, then it takes 4 bytes plus 10 times UTFMax
```

### Table driven tests

Use Table Driven Tests to group multiple cases for the same function. Each test
struct element must be written across multiple lines for readability.

```go
func TestFoo(t *testing.T) {
    tests := []struct {
        name  string
        input int
        want  int
    }{
        {
            name:  "when zero is doubled, then the result is zero",
            input: 0,
            want:  0,
        },
        {
            name:  "when a positive number is doubled, then the result is twice it",
            input: 3,
            want:  6,
        },
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

A single case needs no table, but still needs the wrapper:

```go
func TestLayoutSchema(t *testing.T) {
    t.Run("given a layout built from a schema, when the schema is asked for, then it is the one it was built from", func(t *testing.T) {
        ...
    })
}
```

```go
// Bad
{name: "case1", ...},
{name: "test A", ...},
{name: "returns false when blkNum differs", ...},   // no given, no when

// Good
{name: "given two block ids of the same file, when their block numbers differ, then they are not equal", ...},
```

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
