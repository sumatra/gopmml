package evaluation


type Model interface {
	Verify() error
	Compile() error
	Validate() error
	Evaluate(input DataRow) (DataRow, error)
}