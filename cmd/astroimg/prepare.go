package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/cloudinit"
	"github.com/astrona-io/qcow2-base-image/internal/config"
	"github.com/astrona-io/qcow2-base-image/internal/platform"
	"github.com/astrona-io/qcow2-base-image/internal/qemurun"
)

var prepareCmd = &cobra.Command{
	Use:   "prepare",
	Short: "Generate a local SSH keypair and render the cloud-init user-data/meta-data for this distro/layer",
	RunE: func(cmd *cobra.Command, _ []string) error {
		r, _, err := resolveBuild(cmd)
		if err != nil {
			return err
		}

		return doPrepare(cmd.Context(), r)
	},
}

func doPrepare(ctx context.Context, r config.Resolved) error {
	if err := os.MkdirAll(r.RuntimeDir, 0o750); err != nil {
		return fmt.Errorf("creating %s: %w", r.RuntimeDir, err)
	}

	keyPath := filepath.Join(r.RuntimeDir, "id_ed25519")
	if err := qemurun.GenerateSSHKey(ctx, keyPath); err != nil {
		return fmt.Errorf("generating SSH key: %w", err)
	}

	pub, err := os.ReadFile(keyPath + ".pub") //nolint:gosec // keyPath is internally resolved from build-dir, not raw user input
	if err != nil {
		return fmt.Errorf("reading generated public key: %w", err)
	}

	userDataTmpl, err := os.ReadFile(filepath.Join(r.TemplateDir, "user-data.template"))
	if err != nil {
		return fmt.Errorf("reading user-data template: %w", err)
	}

	rendered := cloudinit.Substitute(string(userDataTmpl), map[string]string{
		"SSH_KEY": strings.TrimSpace(string(pub)),
	})
	if err := os.WriteFile(r.UserData, []byte(rendered), 0o600); err != nil { //nolint:gosec // r.UserData is internally resolved by config.Resolve, not raw user input
		return fmt.Errorf("writing %s: %w", r.UserData, err)
	}

	metaTmpl, err := os.ReadFile(filepath.Join(r.TemplateDir, "meta-data"))
	if err != nil {
		return fmt.Errorf("reading meta-data template: %w", err)
	}

	instanceID := cloudinit.GenerateInstanceID(r.InstanceIDPrefix, time.Now)

	renderedMeta, err := cloudinit.RenderMetaData(string(metaTmpl), instanceID)
	if err != nil {
		return fmt.Errorf("rendering meta-data: %w", err)
	}

	if err := os.WriteFile(r.MetaData, []byte(renderedMeta), 0o600); err != nil { //nolint:gosec // r.MetaData is internally resolved by config.Resolve, not raw user input
		return fmt.Errorf("writing %s: %w", r.MetaData, err)
	}

	// EFI vars is a template file the guest firmware writes boot state
	// into; missing firmware here shouldn't hard-fail prepare (the `run`
	// step gives a clearer, actionable error) so an empty placeholder is
	// written instead, matching the old prepare.sh behavior.
	if code, vars, err := platform.FindEFI(runtime.GOOS, r.Arch); err == nil {
		_ = code

		if err := copyFile(vars, r.VarsFile); err != nil {
			return fmt.Errorf("copying EFI vars: %w", err)
		}

		fmt.Printf("copied %s EFI vars to %s\n", r.Arch, r.VarsFile)
	} else {
		fmt.Printf("warning: QEMU EFI vars not found for %s (%v)\n", r.Arch, err)

		if err := os.WriteFile(r.VarsFile, nil, 0o600); err != nil {
			return fmt.Errorf("writing placeholder %s: %w", r.VarsFile, err)
		}
	}

	fmt.Printf("prepared %s (instance-id=%s)\n", r.ImageTag, instanceID)

	return nil
}
