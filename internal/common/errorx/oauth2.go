package errorx

import (
	"encoding/json"
	"errors"
	"net/http"
)

type OAuth2Error struct {
	ErrorType        string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
	ErrorURI         string `json:"error_uri,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	HTTPStatus       int    `json:"-"`
}

func (e *OAuth2Error) Error() string {
	out, _ := json.Marshal(e)
	return string(out)
}

var (
	ErrInvalidRequest = &OAuth2Error{
		ErrorType:  "invalid_request",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrInvalidClient = &OAuth2Error{
		ErrorType:  "invalid_client",
		HTTPStatus: http.StatusUnauthorized,
	}

	ErrInvalidGrant = &OAuth2Error{
		ErrorType:  "invalid_grant",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrUnauthorizedClient = &OAuth2Error{
		ErrorType:  "unauthorized_client",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrUnsupportedGrantType = &OAuth2Error{
		ErrorType:  "unsupported_grant_type",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrInvalidScope = &OAuth2Error{
		ErrorType:  "invalid_scope",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrInvalidRedirectURI = &OAuth2Error{
		ErrorType:  "invalid_request",
		ErrorCode:  "invalid_redirect_uri",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrClientAlreadyExists = &OAuth2Error{
		ErrorType:  "invalid_request",
		ErrorCode:  "client_already_exists",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrAuthorizationCodeExpired = &OAuth2Error{
		ErrorType:  "invalid_grant",
		ErrorCode:  "code_expired",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrAuthorizationCodeNotFound = &OAuth2Error{
		ErrorType:  "invalid_grant",
		ErrorCode:  "invalid_code",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrTokenExpired = &OAuth2Error{
		ErrorType:  "invalid_token",
		ErrorCode:  "token_expired",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrTokenNotFound = &OAuth2Error{
		ErrorType:  "invalid_token",
		ErrorCode:  "invalid_token",
		HTTPStatus: http.StatusBadRequest,
	}

	ErrOAuth2NotEnabled = &OAuth2Error{
		ErrorType:  "invalid_request",
		ErrorCode:  "oauth2_not_enabled",
		HTTPStatus: http.StatusBadRequest,
	}
)

// ConvertToOAuth2Error converts any error to OAuth2Error
// If the error is already OAuth2Error, return it directly
// Otherwise, wrap it as an unknown error with the original error message
func ConvertToOAuth2Error(err error) *OAuth2Error {
	var oauthErr *OAuth2Error
	if errors.As(err, &oauthErr) {
		return oauthErr
	}

	return &OAuth2Error{
		ErrorType:        "unknown_error",
		ErrorDescription: err.Error(),
		HTTPStatus:       http.StatusInternalServerError,
	}
}
