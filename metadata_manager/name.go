package metadatamanager

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrNameTooLong reports a table, field, view or index name of more characters
// than the catalogs were built to hold.
var ErrNameTooLong = errors.New("name too long")

// maxNameLength is the longest name the catalogs can hold.
//
// One limit covers all four kinds of name, because a name is written into more
// than one catalog and has to fit wherever it goes. An index is the clearest
// case: it is kept as a table, so its name is in the index catalog as an index
// and in the table catalog as a table, and a limit that differed between the
// two would be the smaller of them anyway.
//
// The catalogs are tables, so a name in them is a varchar, and a varchar takes
// up the room its limit allows rather than the room its value needs. Raising
// this is therefore paid for by every catalog record, whether or not any name
// is that long.
const maxNameLength = 16

// checkNameFits refuses a name the catalogs cannot hold. what is the kind of
// name it is, and goes in the message.
//
// A record page would refuse the same name on its own, since the catalogs hold
// names in varchar fields of this width. What is gained by asking here is what
// the caller is told: the record page speaks of a field of a catalog, and what
// the caller passed was the name of a table, a view or an index.
//
// The count is of characters because that is what a varchar's length means.
func checkNameFits(what string, name string) error {
	if count := utf8.RuneCountInString(name); count > maxNameLength {
		return fmt.Errorf("%s name %q is %d characters, and the catalogs hold %d: %w", what, name, count, maxNameLength, ErrNameTooLong)
	}

	return nil
}
