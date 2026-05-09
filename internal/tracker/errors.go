package tracker

import "errors"

var ErrNotImplemented = errors.New("tracker adapter not implemented")

var ErrMutationUnsupported = errors.New("external adapter mutation is not supported; create intake evidence and request approval")
