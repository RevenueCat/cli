package api

import "errors"

// Error-type predicates. Callers should classify API errors through these
// instead of comparing APIError.Type to string literals: the type strings come
// from the OpenAPI spec (and its generated constants), so a spec rename becomes
// a compile error here rather than a silently-dropped match at each call site.
//
// The rate-limit and unauthorized predicates also accept the legacy strings that
// parseError synthesizes for a bodyless 4xx, so they match both a real API
// response and the fallback.

func apiErrorType(err error) (string, bool) {
	var e *APIError
	if errors.As(err, &e) {
		return e.Type, true
	}
	return "", false
}

// IsAlreadyExists reports a 409 resource_already_exists.
func IsAlreadyExists(err error) bool {
	t, ok := apiErrorType(err)
	return ok && t == string(ConflictTypeResourceAlreadyExists)
}

// IsNotFound reports a 404 resource_missing.
func IsNotFound(err error) bool {
	t, ok := apiErrorType(err)
	return ok && t == string(NotFoundTypeResourceMissing)
}

// IsParameterError reports a 400/422 parameter_error.
func IsParameterError(err error) bool {
	t, ok := apiErrorType(err)
	return ok && t == string(BadRequestTypeParameterError)
}

// IsRateLimited reports a 429. The spec type is rate_limit_error; parseError's
// bodyless fallback uses rate_limit_exceeded.
func IsRateLimited(err error) bool {
	t, ok := apiErrorType(err)
	return ok && (t == string(RateLimitedTypeRateLimitError) || t == "rate_limit_exceeded")
}

// IsUnauthorized reports a 401/403. The spec type is authentication_error;
// parseError's bodyless fallback uses unauthorized.
func IsUnauthorized(err error) bool {
	t, ok := apiErrorType(err)
	return ok && (t == string(UnauthorizedTypeAuthenticationError) || t == "unauthorized")
}

// IsServerError reports a 500/502 server_error.
func IsServerError(err error) bool {
	t, ok := apiErrorType(err)
	return ok && t == string(InternalErrorTypeServerError)
}
