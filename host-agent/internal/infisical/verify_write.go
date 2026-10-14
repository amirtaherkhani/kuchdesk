package infisical

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"
)

var ErrVerificationMismatch = errors.New("fixture verification mismatch")
var ErrFixtureCollision = errors.New("fixture name collision")

// VerifyWriteInput intentionally has no secret name or expected value. This
// verifies only a new server-generated fixture, never an existing secret.
type VerifyWriteInput struct {
	ProjectID   string `json:"projectId"`
	Environment string `json:"environment"`
	Path        string `json:"path"`
	Confirm     string `json:"confirm"`
}

type VerifyWriteResult struct {
	FixtureName      string `json:"fixtureName,omitempty"`
	CreatedMatch     bool   `json:"createdMatch"`
	UpdatedMatch     bool   `json:"updatedMatch"`
	MismatchRejected bool   `json:"mismatchRejected"`
	CleanupConfirmed bool   `json:"cleanupConfirmed"`
	Error            string `json:"error,omitempty"`
	CleanupError     string `json:"cleanupError,omitempty"`
}

// VerifySecretWrite exercises the real command/policy boundary and compares
// random non-sensitive markers internally. Values and hashes never escape.
func (s CommandService) VerifySecretWrite(ctx context.Context, input VerifyWriteInput) (result VerifyWriteResult, err error) {
	if !resourceSegment.MatchString(input.ProjectID) || !resourceSegment.MatchString(input.Environment) || !validCommandPath(input.Path) {
		return result, ErrInvalidCommand
	}
	if input.Confirm != "secret.verify_write:"+input.ProjectID+"/"+input.Environment+input.Path {
		return result, ErrConfirmation
	}
	marker := func() (string, error) {
		var b [24]byte
		if _, e := rand.Read(b[:]); e != nil {
			return "", errors.New("fixture random source unavailable")
		}
		return hex.EncodeToString(b[:]), nil
	}
	nonce, err := marker()
	if err != nil {
		return result, err
	}
	first, err := marker()
	if err != nil {
		return result, err
	}
	second, err := marker()
	if err != nil {
		return result, err
	}
	base := Command{ProjectID: input.ProjectID, Environment: input.Environment, Path: input.Path, Name: "KUCHDESK_VERIFY_" + nonce}
	command := func(op string, value *string) Command {
		c := base
		c.Operation = op
		c.Value = value
		target, _, _ := validateCommand(c)
		c.Confirm = op + ":" + target
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// Check all required operations before creating anything. Execution and value
	// comparison recheck policy; this preflight cannot grant later permission.
	for _, c := range []Command{command("secret.create", &first), command("secret.get", nil), command("secret.update", &second), command("secret.delete", nil)} {
		if _, _, _, _, err = s.authorizeCommand(ctx, c); err != nil {
			return result, err
		}
	}
	absent := func(ctx context.Context) (bool, error) {
		_, e := s.Execute(ctx, command("secret.get", nil))
		var api *APIError
		if errors.As(e, &api) && api.StatusCode == http.StatusNotFound {
			return true, nil
		}
		return false, e
	}
	missing, err := absent(ctx)
	if err != nil {
		return result, err
	}
	if !missing {
		return result, ErrFixtureCollision
	}
	result.FixtureName = base.Name
	// Also clean up an ambiguous write response, since absence was proven first.
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
		defer cleanupCancel()
		_, deleteErr := s.Execute(cleanupCtx, command("secret.delete", nil))
		var api *APIError
		if deleteErr != nil && !(errors.As(deleteErr, &api) && api.StatusCode == http.StatusNotFound) {
			result.CleanupError = PublicError(deleteErr).Error()
		} else {
			result.CleanupConfirmed, deleteErr = absent(cleanupCtx)
			if deleteErr != nil {
				result.CleanupError = PublicError(deleteErr).Error()
			} else if !result.CleanupConfirmed {
				result.CleanupError = "fixture_still_present"
			}
		}
		if err != nil {
			result.Error = PublicError(err).Error()
		}
	}()
	if _, err = s.Execute(ctx, command("secret.create", &first)); err != nil {
		return result, err
	}
	compare := func(expected string) (bool, error) {
		_, resolved, _, _, e := s.authorizeCommand(ctx, command("secret.get", nil))
		if e != nil {
			return false, e
		}
		body, e := s.Client.get(ctx, "/api/v4/secrets/"+resolved.Name, url.Values{"projectId": {resolved.ProjectID}, "environment": {resolved.Environment}, "secretPath": {resolved.Path}, "viewSecretValue": {"true"}, "include_imports": {"false"}})
		if e != nil {
			return false, e
		}
		var response struct {
			Secret struct {
				Key   string  `json:"secretKey"`
				Value *string `json:"secretValue"`
			} `json:"secret"`
		}
		if json.Unmarshal(body, &response) != nil || response.Secret.Value == nil || response.Secret.Key != base.Name {
			return false, ErrInvalidResponse
		}
		return subtle.ConstantTimeCompare([]byte(*response.Secret.Value), []byte(expected)) == 1, nil
	}
	result.CreatedMatch, err = compare(first)
	if err != nil {
		return result, err
	}
	if !result.CreatedMatch {
		return result, ErrVerificationMismatch
	}
	var match bool
	match, err = compare(second)
	if err != nil {
		return result, err
	}
	result.MismatchRejected = !match
	if match {
		return result, ErrVerificationMismatch
	}
	if _, err = s.Execute(ctx, command("secret.update", &second)); err != nil {
		return result, err
	}
	result.UpdatedMatch, err = compare(second)
	if err != nil {
		return result, err
	}
	if !result.UpdatedMatch {
		return result, ErrVerificationMismatch
	}
	return result, nil
}
