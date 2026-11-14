package metadatamanager

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrNameTooLong is returned for a table, field, view or index name with more
// characters than the catalogs can store.
var ErrNameTooLong = errors.New("name too long")

// maxNameLength is the maximum length of a name in the catalogs.
//
// One limit applies to all four kinds of name, because a name is written to
// more than one catalog and must fit in each. An index is the clearest case.
// It is stored as a table, so its name is in the index catalog as an index
// name and in the table catalog as a table name. If the two limits differed,
// the smaller one would apply anyway.
//
// The catalogs are tables, so a name in them is a varchar. A varchar takes
// the space of its limit, not of its value. A higher limit therefore
// increases the size of every catalog record, whether or not any name is that
// long.
const maxNameLength = 16

// checkNameFits returns an error for a name that is too long for the
// catalogs. what is the kind of name, and is used in the error message.
//
// A record page would reject the same name, because the catalogs store names
// in varchar fields of this length. The check here gives the caller a better
// error. The record page reports a field of a catalog, while the caller
// passed the name of a table, a view or an index.
//
// The length is counted in characters, because a varchar's declared length is
// a character count.
func checkNameFits(what string, name string) error {
	if count := utf8.RuneCountInString(name); count > maxNameLength {
		return fmt.Errorf("%s name %q is %d characters, and the catalogs hold %d: %w", what, name, count, maxNameLength, ErrNameTooLong)
	}

	return nil
}
