# GoSQL

## Commit Message

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <subject>
```

Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`. Scope is optional.

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

```go
// Bad
{"case1", ...},
{"test A", ...},

// Good
{"returns true when filename and blkNum are equal", ...},
{"returns false when blkNum differs", ...},
```
