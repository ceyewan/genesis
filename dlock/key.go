package dlock

import "github.com/ceyewan/genesis/xerrors"

func validateKey(key string) error {
	if key == "" {
		return xerrors.Wrap(ErrInvalidKey, "key must not be empty")
	}
	return nil
}

func terminalErrorChannel(err error) <-chan error {
	ch := make(chan error, 1)
	ch <- err
	close(ch)
	return ch
}
