package parse

var _ UpdateCommand = CreateViewData{}

// CreateViewData is what a create view statement asks for: a view of the given
// name, standing for the query it was defined as.
type CreateViewData struct {
	viewName string
	qd       QueryData
}

func (CreateViewData) isUpdateCommand() {}

// ViewName is the view to create.
func (d CreateViewData) ViewName() string {
	return d.viewName
}

// ViewDef is the query the view stands for, written out as SQL, which is the
// form a view is stored in.
func (d CreateViewData) ViewDef() string {
	return d.qd.String()
}
