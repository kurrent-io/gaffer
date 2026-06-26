package engine

import (
	"errors"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

type FeedError struct {
	Code        string
	Description string
}

func ClassifyError(err error) FeedError {
	var projErr gafferruntime.ProjectionError
	if errors.As(err, &projErr) {
		return FeedError{
			Code:        projErr.ErrorCode(),
			Description: projErr.ErrorDescription(),
		}
	}
	return FeedError{
		Code:        "unexpected-error",
		Description: err.Error(),
	}
}
