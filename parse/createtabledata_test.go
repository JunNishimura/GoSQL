package parse

import "testing"

func TestCreateTableDataSchema(t *testing.T) {
	t.Run("given create table data of a parsed create table, when a field is added to the schema it returned, then the schema it returns afterwards does not have that field", func(t *testing.T) {
		p := newTestParserAfterCreate(t, "create table student (sid int)")
		ctd, err := p.createTable()
		if err != nil {
			t.Fatalf("createTable() error = %v", err)
		}

		if err := ctd.Schema().AddIntField("gradyear"); err != nil {
			t.Fatalf("AddIntField(%q) error = %v", "gradyear", err)
		}

		if ctd.Schema().HasField("gradyear") {
			t.Errorf("HasField(%q) = true after adding it to a returned schema, want false", "gradyear")
		}
	})
}
