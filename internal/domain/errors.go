package domain

import (
	"fmt"
	"strings"
)

const (
	ErrorInvalidArgument            = "INVALID_ARGUMENT"
	ErrorInvalidNamespace           = "INVALID_NAMESPACE"
	ErrorNamespaceNotFound          = "NAMESPACE_NOT_FOUND"
	ErrorNamespaceConflict          = "NAMESPACE_CONFLICT"
	ErrorPossibleDuplicateNamespace = "POSSIBLE_DUPLICATE_NAMESPACE"
	ErrorMemoryNotFound             = "MEMORY_NOT_FOUND"
	ErrorAmbiguousID                = "AMBIGUOUS_ID"
	ErrorAmbiguousSubject           = "AMBIGUOUS_SUBJECT"
	ErrorInvalidMemoryType          = "INVALID_MEMORY_TYPE"
	ErrorDatabase                   = "DATABASE_ERROR"
	ErrorImport                     = "IMPORT_ERROR"
)

type Error struct {
	Code              string
	Message           string
	Candidates        []string
	SubjectCandidates []SubjectCandidate
	Err               error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Message + ": " + e.Err.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func NewInvalidArgumentError(message string) *Error {
	return &Error{Code: ErrorInvalidArgument, Message: message}
}

func NewInvalidNamespaceError(input string) *Error {
	return &Error{Code: ErrorInvalidNamespace, Message: fmt.Sprintf("Namespace %q is invalid.", input)}
}

func NewNamespaceNotFoundError(target string) *Error {
	return &Error{Code: ErrorNamespaceNotFound, Message: fmt.Sprintf("Namespace %q does not exist.", target)}
}

func NewNamespaceConflictError(message string) *Error {
	return &Error{Code: ErrorNamespaceConflict, Message: message}
}

func NewPossibleDuplicateNamespaceError(candidates []string) *Error {
	return &Error{
		Code:       ErrorPossibleDuplicateNamespace,
		Message:    "A similar namespace already exists.",
		Candidates: candidates,
	}
}

func NewMemoryNotFoundError(id string) *Error {
	return &Error{Code: ErrorMemoryNotFound, Message: fmt.Sprintf("Memory %q does not exist.", id)}
}

func NewAmbiguousIDError(id string) *Error {
	return &Error{
		Code:    ErrorAmbiguousID,
		Message: fmt.Sprintf("Memory id prefix %q matches multiple memories.", id),
	}
}

func NewAmbiguousSubjectError(subject string, candidates []SubjectCandidate) *Error {
	return &Error{
		Code:              ErrorAmbiguousSubject,
		Message:           fmt.Sprintf("Related memory subject %q matches multiple memories.", subject),
		SubjectCandidates: candidates,
	}
}

func NewRelatedMemoryNotFoundError(id, namespace string) *Error {
	return &Error{
		Code:    ErrorInvalidArgument,
		Message: fmt.Sprintf("Related identifier %q not found as id or subject in namespace %q.", id, namespace),
	}
}

func NewInvalidMemoryTypeError(input string) *Error {
	return &Error{
		Code:    ErrorInvalidMemoryType,
		Message: fmt.Sprintf("Memory type %q is invalid; valid types are %s.", input, strings.Join(ValidMemoryTypes(), ", ")),
	}
}

func NewImportError(message string, err error) *Error {
	return &Error{Code: ErrorImport, Message: message, Err: err}
}
