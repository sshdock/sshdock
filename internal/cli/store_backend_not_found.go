package cli

import "fmt"

type appNotFoundError string

func (e appNotFoundError) Error() string {
	return fmt.Sprintf("app %q not found", string(e))
}

func (appNotFoundError) Unwrap() error {
	return ErrNotFound
}
