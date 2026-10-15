// Package errs defines the error hierarchy used across the migration framework.
//
// Every expected, user-actionable failure is represented by an *Error carrying a
// Category so that callers and the CLI can distinguish configuration mistakes,
// Kustomize build failures, generation problems, and validation mismatches.
package errs

import "fmt"

// Category classifies a migration failure.
type Category int

const (
	// Migration is the base category for unexpected migration failures.
	Migration Category = iota
	// Configuration indicates an invalid input configuration or option.
	Configuration
	// Build indicates that Kustomize could not render an input configuration.
	Build
	// Generation indicates that a Helm chart could not be generated safely.
	Generation
	// Validation indicates a generated chart is invalid or changes semantics.
	Validation
)

// String returns a human-readable category name.
func (c Category) String() string {
	switch c {
	case Configuration:
		return "configuration"
	case Build:
		return "build"
	case Generation:
		return "generation"
	case Validation:
		return "validation"
	default:
		return "migration"
	}
}

// Error is the framework's structured error type.
type Error struct {
	Category Category
	Message  string
	Cause    error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return e.Category.String() + " error"
}

// Unwrap exposes the wrapped cause for errors.Is/errors.As.
func (e *Error) Unwrap() error { return e.Cause }

func newError(cat Category, cause error, format string, args ...any) *Error {
	return &Error{Category: cat, Message: fmt.Sprintf(format, args...), Cause: cause}
}

// Configurationf builds a configuration error.
func Configurationf(format string, args ...any) *Error {
	return newError(Configuration, nil, format, args...)
}

// Buildf builds a Kustomize build error.
func Buildf(format string, args ...any) *Error {
	return newError(Build, nil, format, args...)
}

// Generationf builds a chart generation error.
func Generationf(format string, args ...any) *Error {
	return newError(Generation, nil, format, args...)
}

// Validationf builds a validation error.
func Validationf(format string, args ...any) *Error {
	return newError(Validation, nil, format, args...)
}

// Migrationf builds a generic migration error.
func Migrationf(format string, args ...any) *Error {
	return newError(Migration, nil, format, args...)
}

// Wrap attaches a cause to a categorized error.
func Wrap(cat Category, cause error, format string, args ...any) *Error {
	return newError(cat, cause, format, args...)
}

// Is reports whether err is an *Error with the given category.
func Is(err error, cat Category) bool {
	e, ok := err.(*Error)
	return ok && e.Category == cat
}
