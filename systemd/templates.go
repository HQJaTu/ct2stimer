package systemd

import (
	"embed"

	"github.com/pkg/errors"
)

// templateFS embeds the systemd unit templates into the binary. It replaces the
// go-bindata-generated bindata.go that older versions of this project relied on.
//
//go:embed templates/*.tmpl
var templateFS embed.FS

// Asset returns the contents of the named embedded template. The name is kept
// path-relative to the package (e.g. "templates/service.tmpl") to preserve the
// original go-bindata API used throughout this package.
func Asset(name string) ([]byte, error) {
	body, err := templateFS.ReadFile(name)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read embedded asset %q", name)
	}

	return body, nil
}
