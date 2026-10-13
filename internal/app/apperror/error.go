package apperror

type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string { return e.Message }

func New(status int, code, message string) error {
	return &Error{Status: status, Code: code, Message: message}
}
