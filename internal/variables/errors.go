package variables

type HTTPError struct {
	Msg    string
	Status int
}

func (e HTTPError) Error() string   { return e.Msg }
func (e HTTPError) HTTPStatus() int { return e.Status }
