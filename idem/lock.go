package idem

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/ceyewan/genesis/xerrors"
)

const lockTokenSize = 16

func newLockToken() (LockToken, error) {
	b := make([]byte, lockTokenSize)
	if _, err := rand.Read(b); err != nil {
		return "", xerrors.Wrap(err, "idem: generate lock token")
	}
	return LockToken(hex.EncodeToString(b)), nil
}
