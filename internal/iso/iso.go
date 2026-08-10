// Package iso builds the cloud-init NoCloud "cidata" seed ISO in pure Go,
// replacing the old macOS-only `hdiutil makehybrid` step so astroimg has no
// external ISO-tool dependency on any platform (hdiutil doesn't exist on
// Linux, and genisoimage/xorriso aren't guaranteed to be installed on CI
// runners either).
package iso

import (
	"bytes"
	"fmt"
	"os"

	"github.com/kdomanski/iso9660"
)

// volumeIdentifier must stay "cidata": that's the label cloud-init's
// NoCloud datasource scans attached volumes for.
const volumeIdentifier = "cidata"

// WriteCIData writes an ISO9660 image at outPath containing exactly
// "user-data" and "meta-data" at the root, labeled "cidata". Both cloud-init
// files fit well within this library's 30-character plain-ISO9660 filename
// limit and its extended (non-standard-but-Linux-compatible) character set,
// so no Joliet/Rock Ridge extension is needed to keep the exact filenames
// and hyphens cloud-init expects.
func WriteCIData(outPath string, userData, metaData []byte) error {
	writer, err := iso9660.NewWriter()
	if err != nil {
		return fmt.Errorf("creating iso writer: %w", err)
	}
	defer func() { _ = writer.Cleanup() }()

	if err := writer.AddFile(bytes.NewReader(userData), "user-data"); err != nil {
		return fmt.Errorf("adding user-data: %w", err)
	}

	if err := writer.AddFile(bytes.NewReader(metaData), "meta-data"); err != nil {
		return fmt.Errorf("adding meta-data: %w", err)
	}

	out, err := os.OpenFile(outPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0o600) //nolint:gosec // outPath is internally resolved, not raw user input
	if err != nil {
		return fmt.Errorf("creating %s: %w", outPath, err)
	}

	if err := writer.WriteTo(out, volumeIdentifier); err != nil {
		_ = out.Close()
		return fmt.Errorf("writing iso: %w", err)
	}

	return out.Close()
}
