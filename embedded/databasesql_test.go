package embedded_test

import (
	"database/sql"
	"reflect"
	"testing"

	_ "github.com/JunNishimura/GoSQL/embedded"
)

// person is a record of the table people.
type person struct {
	id   int
	name string
}

// openTestDB opens a database of its own through database/sql, closed when the
// test ends.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("gosql", t.TempDir())
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})

	return db
}

// exec runs query through db and reports how many records it changed.
func exec(t *testing.T, db interface {
	Exec(query string, args ...any) (sql.Result, error)
}, query string) int64 {
	t.Helper()

	res, err := db.Exec(query)
	if err != nil {
		t.Fatalf("Exec(%q) error = %v", query, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		t.Fatalf("RowsAffected() error = %v", err)
	}

	return n
}

// queryPeople reads every record of the table people through db.
func queryPeople(t *testing.T, db interface {
	Query(query string, args ...any) (*sql.Rows, error)
}) []person {
	t.Helper()

	rows, err := db.Query("select id, name from people")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	defer rows.Close()

	var people []person
	for rows.Next() {
		var p person
		if err := rows.Scan(&p.id, &p.name); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		people = append(people, p)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Err() error = %v", err)
	}

	return people
}

func TestDatabaseSQL(t *testing.T) {
	t.Run("given a database opened through database/sql, when a table is made and records are put in, changed and deleted, then each change reports one record and a query reads back what is left", func(t *testing.T) {
		db := openTestDB(t)
		exec(t, db, "create table people (id int, name varchar(10))")
		exec(t, db, "insert into people (id, name) values (1, 'ann')")
		exec(t, db, "insert into people (id, name) values (2, 'bob')")
		exec(t, db, "insert into people (id, name) values (3, 'cid')")

		if n := exec(t, db, "update people set name = 'bobby' where id = 2"); n != 1 {
			t.Errorf("the update reports %d records changed, want 1", n)
		}
		if n := exec(t, db, "delete from people where id = 3"); n != 1 {
			t.Errorf("the delete reports %d records changed, want 1", n)
		}

		want := []person{
			{
				id:   1,
				name: "ann",
			},
			{
				id:   2,
				name: "bobby",
			},
		}
		if got := queryPeople(t, db); !reflect.DeepEqual(got, want) {
			t.Errorf("the query reads %v, want %v", got, want)
		}
	})

	t.Run("given a database opened through database/sql, when the column types of a query are asked for, then they carry each column's type and length", func(t *testing.T) {
		db := openTestDB(t)
		exec(t, db, "create table people (id int, name varchar(10))")
		rows, err := db.Query("select id, name from people")
		if err != nil {
			t.Fatalf("Query() error = %v", err)
		}
		defer rows.Close()

		types, err := rows.ColumnTypes()
		if err != nil {
			t.Fatalf("ColumnTypes() error = %v", err)
		}

		type columnType struct {
			name     string
			typeName string
			length   int64
			hasLen   bool
		}
		var got []columnType
		for _, ct := range types {
			length, ok := ct.Length()
			got = append(got, columnType{
				name:     ct.Name(),
				typeName: ct.DatabaseTypeName(),
				length:   length,
				hasLen:   ok,
			})
		}
		want := []columnType{
			{
				name:     "id",
				typeName: "INT",
				length:   0,
				hasLen:   false,
			},
			{
				name:     "name",
				typeName: "VARCHAR",
				length:   10,
				hasLen:   true,
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ColumnTypes() = %+v, want %+v", got, want)
		}
	})
}

func TestDatabaseSQLTx(t *testing.T) {
	t.Run("given a table, when a record is put in within a transaction begun through database/sql that commits, then a query reads it", func(t *testing.T) {
		db := openTestDB(t)
		exec(t, db, "create table people (id int, name varchar(10))")
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		exec(t, tx, "insert into people (id, name) values (1, 'ann')")

		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit() error = %v", err)
		}

		want := []person{
			{
				id:   1,
				name: "ann",
			},
		}
		if got := queryPeople(t, db); !reflect.DeepEqual(got, want) {
			t.Errorf("the query reads %v, want %v", got, want)
		}
	})

	t.Run("given a table, when a record is put in within a transaction begun through database/sql that rolls back, then a query reads none", func(t *testing.T) {
		db := openTestDB(t)
		exec(t, db, "create table people (id int, name varchar(10))")
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		exec(t, tx, "insert into people (id, name) values (1, 'ann')")
		if got := queryPeople(t, tx); len(got) != 1 {
			t.Fatalf("the transaction reads %v before rolling back, want the record it put in", got)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}

		if got := queryPeople(t, db); len(got) != 0 {
			t.Errorf("the query reads %v, want none", got)
		}
	})
}

func TestDatabaseSQLReopen(t *testing.T) {
	t.Run("given a database closed after a record was put in and committed, when it is opened again through database/sql, then a query reads the record", func(t *testing.T) {
		dir := t.TempDir()
		db, err := sql.Open("gosql", dir)
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}
		exec(t, db, "create table people (id int, name varchar(10))")
		exec(t, db, "insert into people (id, name) values (1, 'ann')")
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		reopened, err := sql.Open("gosql", dir)
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}
		defer reopened.Close()

		want := []person{
			{
				id:   1,
				name: "ann",
			},
		}
		if got := queryPeople(t, reopened); !reflect.DeepEqual(got, want) {
			t.Errorf("the query reads %v, want %v", got, want)
		}
	})

	t.Run("given a database closed while a transaction that put in a record was still open, when it is opened again through database/sql, then a query reads none", func(t *testing.T) {
		dir := t.TempDir()
		db, err := sql.Open("gosql", dir)
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}
		exec(t, db, "create table people (id int, name varchar(10))")
		tx, err := db.Begin()
		if err != nil {
			t.Fatalf("Begin() error = %v", err)
		}
		exec(t, tx, "insert into people (id, name) values (1, 'ann')")
		if err := db.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}

		reopened, err := sql.Open("gosql", dir)
		if err != nil {
			t.Fatalf("sql.Open() error = %v", err)
		}
		defer reopened.Close()

		if got := queryPeople(t, reopened); len(got) != 0 {
			t.Errorf("the query reads %v, want none", got)
		}
	})
}
