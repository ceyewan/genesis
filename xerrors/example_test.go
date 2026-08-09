package xerrors_test

import "github.com/ceyewan/genesis/xerrors"

func Example() {
	base := xerrors.New("not found")
	_ = xerrors.Wrap(base, "load user")
}
