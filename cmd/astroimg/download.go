package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/imagefetch"
)

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download the upstream cloud image, verify its checksum, and convert it into a pristine base disk (base builds only)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		return doDownload(cmd.Context(), r)
	},
}

func doDownload(ctx context.Context, r config.Resolved) error {
	if r.Layer != "" {
		fmt.Printf("layer=%s set: skipping download, layers build on top of an existing base image (%s)\n", r.Layer, r.LayerBaseImage)
		return nil
	}

	if err := os.MkdirAll(r.RuntimeDir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", r.RuntimeDir, err)
	}

	if _, err := os.Stat(r.DownloadedImg); errors.Is(err, os.ErrNotExist) {
		if err := downloadAndVerify(ctx, r); err != nil {
			return err
		}
	} else {
		fmt.Printf("%s already exists\n", r.DownloadedImg)
	}

	if _, err := os.Stat(r.BaseDisk); errors.Is(err, os.ErrNotExist) {
		fmt.Println("converting downloaded image to pristine qcow2 base disk...")

		if err := runExternal(ctx, "qemu-img", "convert", "-f", "qcow2", "-O", "qcow2", r.DownloadedImg, r.BaseDisk); err != nil {
			return err
		}

		fmt.Println("resizing pristine base disk to 25G...")

		if err := runExternal(ctx, "qemu-img", "resize", r.BaseDisk, "25G"); err != nil {
			return err
		}
	} else {
		fmt.Printf("%s already exists\n", r.BaseDisk)
	}

	return nil
}

// downloadAndVerify fetches the upstream image and, if the distro publishes
// one, verifies it against a SHA256SUMS manifest before it's trusted.
func downloadAndVerify(ctx context.Context, r config.Resolved) error {
	fmt.Printf("downloading %s...\n", r.ImageURL)

	if err := imagefetch.Download(ctx, nil, r.ImageURL, r.DownloadedImg); err != nil {
		return err
	}

	if r.ChecksumURL == "" {
		return nil
	}

	fmt.Println("fetching checksum manifest...")

	sums, err := imagefetch.FetchText(ctx, nil, r.ChecksumURL)
	if err != nil {
		return fmt.Errorf("fetching checksum manifest: %w", err)
	}

	filename := path.Base(r.ImageURL)
	if err := imagefetch.VerifyFile(sums, filename, r.DownloadedImg); err != nil {
		// Don't leave a file behind that looks "already downloaded" on the
		// next run -- force a fresh download instead.
		if rmErr := os.Remove(r.DownloadedImg); rmErr != nil {
			return fmt.Errorf("checksum verification failed (%w) and cleanup failed: %w", err, rmErr)
		}

		return fmt.Errorf("checksum verification failed, removed downloaded file: %w", err)
	}

	fmt.Println("checksum verified")

	return nil
}
