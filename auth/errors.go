package auth

import "github.com/ceyewan/genesis/xerrors"

var (
	ErrInvalidToken     = xerrors.New("auth: invalid token")
	ErrExpiredToken     = xerrors.New("auth: token expired")
	ErrMissingToken     = xerrors.New("auth: missing token")
	ErrInvalidClaims    = xerrors.New("auth: invalid claims")
	ErrInvalidSignature = xerrors.New("auth: invalid signature")
	ErrInvalidConfig    = xerrors.New("auth: invalid config")
)
