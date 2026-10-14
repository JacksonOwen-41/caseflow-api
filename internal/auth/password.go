package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

const (
	minPasswordBytes = 12
	maxPasswordBytes = 72 // bcrypt ignores input beyond this limit.
	passwordCost     = 12
)

var ErrInvalidPassword = errors.New("password must be between 12 and 72 bytes")

func HashPassword(password string) (string, error) {
	if len(password) < minPasswordBytes || len(password) > maxPasswordBytes {
		return "", ErrInvalidPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	if len(password) > maxPasswordBytes {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}