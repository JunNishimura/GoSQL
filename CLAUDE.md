# GoSQL

## Development Workflow

Follow the TDD cycle: **List → Red → Green → Refactor**

- **List**: enumerate the tasks to work on and choose which one to tackle. Skip this step if the task is already clear.
- **Red**: write failing tests, run them, and confirm they fail, then stop and wait for user confirmation before proceeding
  - A compile error does not count as a failing test. If the tests call a function, method, or type that does not exist yet, add a stub with the final signature and an empty body that returns zero values, so the tests compile and run.
  - Confirm each new test fails on its assertions, for the reason the test is meant to check. A test that already passes against the stub is not checking anything yet.
- **Green**: implement the minimum code to make the tests pass
- **Refactor**: clean up the code while keeping tests green

Do NOT proceed from Red to Green without explicit user approval.

## Comments

Write all comments in English. This applies to Go doc comments, inline comments, and comments in config files such as `.github/workflows/*.yml` and `.golangci.yml`.

Commit message bodies are the exception: those may be written in Japanese.

### Style

Write comments in plain technical English, so that a reader understands each
sentence on the first pass.

- **One fact per sentence.** Split a sentence that carries a condition, its
  consequence and the reason for it.
- **At most one level of relative clause.** A clause nested inside another
  clause (`a tree whose directory says which leaf a value is in`) becomes a
  second sentence.
- **Prefer the technical term to an everyday phrase.** Write "keyed by",
  "releases", "descends to", not "filed under", "gives back", "goes down".
- **Put the subject first.** Write "Any index opened before the failure is
  closed", not "What was opened before a failure is closed again".
- **Use one term for one concept.** Once a comment calls something the key, it
  does not later become "the value it is filed under".

```go
// Bad
// BTreeIndex is an index that keeps its entries in order of the value they are
// filed under, in the leaves of a tree whose directory says which leaf a value
// is in.

// Good
// BTreeIndex stores its entries in leaf blocks, sorted by key.
// A directory tree maps each key to the leaf that holds it.
```

## Commit Message

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <subject>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`. Scope is optional.

Keep the body to five lines at most. State why the change was made, not what
the diff already shows. Put longer reasoning in a doc comment or a pull request
description, where it stays next to the code or the discussion.

## Unit Tests

### Where the intent goes

Name the test function after the unit under test only: `TestSchemaAddField`,
not `TestSchemaAddFieldKeepsTheCharacterLimit`. The case name states what a
case checks. The function name only identifies the function under test.

Run every case inside `t.Run`, including a test with a single case. The intent
of a case belongs in the subtest name, so a single case also needs a subtest
name. Do not put the intent in the function name.

### Case names

Write a case name as **given / when / then**: the initial condition, the call
under test, and the expected result.

```
given a schema with an int field, when a varchar field of the same name is added, then it reports ErrDuplicateField
```

The three parts are a default, not a required form. The requirement is that a
reader can tell what a failing case checked without opening the test body. Omit
a part that adds no information. A name padded to three clauses is harder to
read than the short name, and no clearer.

Omit `given` when the case has no initial condition. A pure function of its
arguments has none.

```
when a string of 10 characters is measured, then it takes 4 bytes plus 10 times UTFMax
```

Omit `when` if it would only repeat the `then`. This is usual for a
constructor: the case makes one call and checks its result.

```
// Padded
when a block id is made from a file name and a block number, then it carries both

// Better
it carries the file name and the block number it was made from
```

If every case in a table shares a `given`, repeat it in each case name. Do not
state it once elsewhere. A case name is read alone, in a line of test output,
without the table or the comments around it.

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
