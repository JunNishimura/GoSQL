package parse

// UpdateCommand is a statement that changes the database rather than reading
// from it, as returned by Parser.UpdateCmd.
//
// Which statement it is comes out of a type switch on the concrete type. The
// method is unexported so that the statements this package parses are the only
// ones there are.
type UpdateCommand interface {
	isUpdateCommand()
}
