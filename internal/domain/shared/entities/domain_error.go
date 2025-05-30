package entities

// DomainError represents domain-specific errors that can be shared across contexts
type DomainError struct {
	Message string
}

func (e DomainError) Error() string {
	return e.Message
}
