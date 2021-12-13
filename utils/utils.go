package utils

import (
	"context"
	"errors"
	"time"
)

func WaitFor(stopChan chan struct{}, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return false
	case <-stopChan:
		return true
	}
}

type ActionResult chan error

var successErr = errors.New("success")

func NewActionResult() ActionResult {
	return make(ActionResult)
}

func (e ActionResult) Success() {
	e.Error(successErr)
}

func (e ActionResult) Error(err error) {
	// do not hang on second error if we already have one
	select {
	case e <- err:
	default:
	}
}

func (e ActionResult) Result() error {
	err := <-e
	if err == successErr {
		return nil
	}
	return err
}
