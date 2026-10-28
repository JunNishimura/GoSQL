package materialize

import (
	"testing"

	"github.com/JunNishimura/GoSQL/parse"
)

func TestNewTempTable(t *testing.T) {
	t.Run("given a temp table already made, when a second is made, then its name differs", func(t *testing.T) {
		tx := newTestTransaction(t)
		schema := newTestSchema(t)

		first := NewTempTable(tx, schema)
		second := NewTempTable(tx, schema)

		if first.TableName() == "" {
			t.Fatal("the first temp table has no name")
		}
		if first.TableName() == second.TableName() {
			t.Errorf("both temp tables are named %q, want different names", first.TableName())
		}
	})

	// A user's table of the same name would share the temp table's file, and
	// would be deleted along with the temp tables when the database starts.
	t.Run("its name is not one a statement can name a table by", func(t *testing.T) {
		tx := newTestTransaction(t)

		name := NewTempTable(tx, newTestSchema(t)).TableName()
		if name == "" {
			t.Fatal("the temp table has no name")
		}

		p, err := parse.NewParser("select id from " + name)
		if err != nil {
			t.Fatalf("NewParser() error = %v", err)
		}
		if _, err := p.Query(); err == nil {
			t.Errorf("a query from %q parsed, want it refused", name)
		}
	})

	t.Run("its layout is of the schema it was made with", func(t *testing.T) {
		tx := newTestTransaction(t)
		schema := newTestSchema(t)

		layout := NewTempTable(tx, schema).Layout()
		if layout == nil {
			t.Fatal("Layout() = nil, want a layout")
		}
		if layout.Schema() != schema {
			t.Errorf("Layout().Schema() = %p, want %p", layout.Schema(), schema)
		}
	})
}

func TestTempTableOpen(t *testing.T) {
	t.Run("given records written through one scan of a temp table, when a second scan is opened, then it reads them back", func(t *testing.T) {
		tx := newTestTransaction(t)
		tt := NewTempTable(tx, newTestSchema(t))

		w, err := tt.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if w == nil {
			t.Fatal("Open() = nil, want a scan")
		}
		for _, id := range []int32{3, 1, 2} {
			if err := w.MoveToNewRecord(); err != nil {
				t.Fatalf("MoveToNewRecord() error = %v", err)
			}
			if err := w.SetInt("id", id); err != nil {
				t.Fatalf("SetInt() error = %v", err)
			}
		}
		w.Close()

		r, err := tt.Open()
		if err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		defer r.Close()

		got := []int32{}
		for {
			ok, err := r.MoveToNextRecord()
			if err != nil {
				t.Fatalf("MoveToNextRecord() error = %v", err)
			}
			if !ok {
				break
			}
			id, err := r.GetInt("id")
			if err != nil {
				t.Fatalf("GetInt() error = %v", err)
			}
			got = append(got, id)
		}

		want := []int32{3, 1, 2}
		if len(got) != len(want) {
			t.Fatalf("read ids %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("read ids %v, want %v", got, want)
				break
			}
		}
	})
}
