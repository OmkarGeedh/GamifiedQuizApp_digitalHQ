package dto

import "errors"

// DeleteAccountRequestDTO confirms an account deletion with the password, so a
// stolen or left-open session cannot delete someone's account.
type DeleteAccountRequestDTO struct {
	Password string `json:"password"`
}

// Validate checks the password is present and of sane length.
func (r *DeleteAccountRequestDTO) Validate() error {
	if r.Password == "" {
		return errors.New("password is required to delete the account")
	}
	if len(r.Password) > 128 {
		return errors.New("password is invalid")
	}
	return nil
}
