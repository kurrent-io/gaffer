package engine

import gafferruntime "github.com/kurrent-io/gaffer/bindings/go"

type FeedError struct {
	Code        string
	Description string
}

func ClassifyError(err error) FeedError {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
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
