package parse

var _ UpdateCommand = CreateViewData{}

// CreateViewData is the content of a create view statement: a view with the
// given name, defined by a query.
type CreateViewData struct {
	viewName string
	qd       QueryData
}

func (CreateViewData) isUpdateCommand() {}

// ViewName returns the name of the view to create.
func (d CreateViewData) ViewName() string {
	return d.viewName
}

// ViewDef returns the query of the view as SQL text, which is the form in
// which a view is stored.
func (d CreateViewData) ViewDef() string {
	return d.qd.String()
}
