# GoSQL

GoSQL is a small relational database written in Go. It is a port of
SimpleDB, the teaching database from Edward Sciore's book
[*Database Design and Implementation*](#references).

It is a learning project, not a production database.

## Features

- Block-based storage with a buffer pool
- Write-ahead logging with undo-only recovery and checkpoints
- Transactions with shared and exclusive block-level locks
- A SQL subset: `create table` / `view` / `index`, `insert`, `update`, `delete`, `select`
- Static hash and B-tree indexes
- A heuristic query planner and an index-aware update planner
- An embedded `database/sql` driver

## Architecture

The packages form layers, listed here from the bottom up.
Each layer uses only the layers below it.

| Package | Role |
| --- | --- |
| [`file_manager`](file_manager) | Reads and writes fixed-size blocks of a file |
| [`log_manager`](log_manager) | Appends records to the log file and iterates over them |
| [`buffer_manager`](buffer_manager) | Caches blocks in a pool of buffers and pins them |
| [`recovery_manager`](recovery_manager) | Writes log records and rolls back or recovers transactions |
| [`concurrency_manager`](concurrency_manager) | Grants block-level locks through a lock table |
| [`transaction`](transaction) | Combines recovery, concurrency and buffers into one transaction |
| [`record_manager`](record_manager) | Lays out fixed-size records in a block |
| [`query`](query) | Scans, predicates and expressions |
| [`index`](index) | Hash and B-tree indexes and the scans that use them |
| [`materialize`](materialize) | Temporary tables, sorting, grouping and merge join |
| [`multibuffer`](multibuffer) | Products that read several blocks at once |
| [`metadata_manager`](metadata_manager) | Keeps the table, view, index and statistics catalogs |
| [`parse`](parse) | Lexer and recursive-descent parser for SQL |
| [`plan`](plan) | Query and update planners and their plans |
| [`server`](server) | Opens a database on a directory and recovers it |
| [`embedded`](embedded) | `database/sql` driver that runs the database in-process |

## Usage

Requires Go 1.26 or later.

```sh
go get github.com/JunNishimura/GoSQL
```

Import the `embedded` package to register the `gosql` driver.
The data source name is the directory that stores the database.
The directory is created if it does not exist.

```go
package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/JunNishimura/GoSQL/embedded"
)

func main() {
	db, err := sql.Open("gosql", "studentdb")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	for _, stmt := range []string{
		"create table student (sid int, sname varchar(10), gradyear int)",
		"insert into student (sid, sname, gradyear) values (1, 'joe', 2021)",
		"insert into student (sid, sname, gradyear) values (2, 'amy', 2020)",
	} {
		if _, err := db.Exec(stmt); err != nil {
			log.Fatal(err)
		}
	}

	rows, err := db.Query("select sid, sname from student where gradyear = 2020")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var sid int64
		var sname string
		if err := rows.Scan(&sid, &sname); err != nil {
			log.Fatal(err)
		}
		fmt.Println(sid, sname) // 2 amy
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
}
```

`sql.Open` uses a block size of 4096 bytes and a pool of 32 buffers.
To set other sizes, create a connector and pass it to `sql.OpenDB`:

```go
c, err := embedded.NewConnector("studentdb", 1024, 8)
if err != nil {
	log.Fatal(err)
}
db := sql.OpenDB(c)
```

A statement outside `db.Begin` runs in a transaction of its own.

### SQL limitations

GoSQL supports only the SQL subset that SimpleDB supports.

- The column types are `int` and `varchar(n)`.
- `select` lists its columns by name. It does not accept `*`, `order by` or `group by`.
- A `where` clause joins terms of the form `a = b` with `and`. It does not accept `or`, `<` or `>`.
- Statements do not accept placeholders such as `?`.
- `LastInsertId` is not supported.

## Development

```sh
go test -race ./...
golangci-lint run
```

## References

- Edward Sciore, [*Database Design and Implementation*](https://link.springer.com/book/10.1007/978-3-030-33836-7), 2nd ed., Springer, 2020.

## License

[MIT](LICENSE)
