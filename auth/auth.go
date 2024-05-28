package auth

import "github.com/pquerna/otp/totp"

type AuthCheck interface {
	Verify(code string) (bool, error)
}

func NewBasic(code string) AuthCheck {
	return authBasic{
		code: code,
	}
}

type authBasic struct {
	code string
}

func (a authBasic) Verify(code string) (bool, error) {
	return code == a.code, nil
}

func NewTOTP(secret string) AuthCheck {
	return authTOTP{
		secret: secret,
	}
}

type authTOTP struct {
	secret string
}

func (a authTOTP) Verify(code string) (bool, error) {
	return totp.Validate(code, a.secret), nil
}
