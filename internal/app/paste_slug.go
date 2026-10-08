package app

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"regexp"
)

var customSlugPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

const errInvalidSlug = errString("Custom slugs must be 1–64 letters, numbers, hyphens or underscores, starting with a letter or number.")
const errSlugTaken = errString("That slug is already in use. Choose another.")
const errInvalidSlugLength = errString("Random slug length must be long or short.")

type pasteSlugOptions struct {
	Slug       string `json:"slug"`
	SlugLength string `json:"slug_length"`
}

func (options pasteSlugOptions) publicID() (string, error) {
	if options.SlugLength != "" && options.SlugLength != "long" && options.SlugLength != "short" {
		return "", errInvalidSlugLength
	}
	if options.Slug != "" {
		if !customSlugPattern.MatchString(options.Slug) {
			return "", errInvalidSlug
		}
		return options.Slug, nil
	}
	if options.SlugLength == "short" {
		id := make([]byte, 8)
		for i := range id {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(base62Alphabet))))
			if err != nil {
				return "", err
			}
			id[i] = base62Alphabet[n.Int64()]
		}
		return string(id), nil
	}
	return newPublicID()
}

func writePasteCreateError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "internal error"
	if err == errInvalidSlug {
		status, code, message = http.StatusBadRequest, "invalid_slug", err.Error()
	}
	if err == errSlugTaken {
		status, code, message = http.StatusConflict, "slug_taken", err.Error()
	}
	if err == errInvalidSlugLength {
		status, code, message = http.StatusBadRequest, "invalid_slug_length", err.Error()
	}
	writePasteError(w, r, status, code, message)
}
