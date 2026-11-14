package parse

// UpdateCommand is a statement that changes the database instead of reading
// from it. Parser.UpdateCmd returns one.
//
// A caller finds out which statement it is with a type switch on the concrete
// type. The interface method is unexported, so that only the statements
// parsed by this package implement it.
type UpdateCommand interface {
	isUpdateCommand()
}
