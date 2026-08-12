package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/astrona-io/qcow2-base-image/internal/config"
)

var flagGetOutput string

var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Human-readable table view of distros or layers (kubectl-style)",
}

func init() {
	getCmd.PersistentFlags().StringVarP(&flagGetOutput, "output", "o", "", "output format: wide")

	getCmd.AddCommand(
		&cobra.Command{
			Use:     "distro",
			Aliases: []string{"distros"},
			Short:   "Table of available distros (name, image variant, releases)",
			RunE: func(_ *cobra.Command, _ []string) error {
				wide, err := parseGetOutput(flagGetOutput)
				if err != nil {
					return err
				}

				return getDistros(wide)
			},
		},
		&cobra.Command{
			Use:     "layer",
			Aliases: []string{"layers"},
			Short:   "Table of available layers for --distro",
			RunE: func(_ *cobra.Command, _ []string) error {
				wide, err := parseGetOutput(flagGetOutput)
				if err != nil {
					return err
				}

				return getLayers(flagDistro, wide)
			},
		},
		&cobra.Command{
			Use:     "os-version",
			Aliases: []string{"os-versions"},
			Short:   "Table of available OS-VERSION values for --distro (distros/<distro>/distro.yaml releases:)",
			RunE: func(_ *cobra.Command, _ []string) error {
				wide, err := parseGetOutput(flagGetOutput)
				if err != nil {
					return err
				}

				return getOSVersions(flagDistro, wide)
			},
		},
	)
}

func parseGetOutput(v string) (bool, error) {
	switch v {
	case "":
		return false, nil
	case "wide":
		return true, nil
	default:
		return false, fmt.Errorf("invalid -o value %q: only \"wide\" supported", v)
	}
}

func getDistros(wide bool) error {
	entries, err := os.ReadDir(distrosRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	if wide {
		fmt.Fprintln(w, "NAME\tVARIANT\tRELEASES\tSSH_USER\tIMAGE_URL_TEMPLATE")
	} else {
		fmt.Fprintln(w, "NAME\tVARIANT\tRELEASES")
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()

		cfg, _, err := config.LoadDistro(distrosRoot, name, "")
		if err != nil {
			fmt.Fprintf(w, "%s\t<error>\t%s\n", name, err)
			continue
		}

		versions, err := config.ListReleases(distrosRoot, name)
		if err != nil {
			fmt.Fprintf(w, "%s\t%s\t<error: %s>\n", name, cfg.ImageVariant, err)
			continue
		}

		if wide {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, cfg.ImageVariant, strings.Join(versions, ","), cfg.SSHUser, cfg.ImageURLTemplate)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\n", name, cfg.ImageVariant, strings.Join(versions, ","))
		}
	}

	return w.Flush()
}

func getLayers(distro string, wide bool) error {
	root := filepath.Join(distrosRoot, distro, "layers")

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}

		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	if wide {
		fmt.Fprintln(w, "NAME\tDISTRO\tUSER_DATA_TEMPLATE\tPATH")
	} else {
		fmt.Fprintln(w, "NAME\tDISTRO")
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		if wide {
			layerPath := filepath.Join(root, e.Name())

			hasUserData := "no"
			if _, err := os.Stat(filepath.Join(layerPath, "user-data.template")); err == nil {
				hasUserData = "yes"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Name(), distro, hasUserData, layerPath)
		} else {
			fmt.Fprintf(w, "%s\t%s\n", e.Name(), distro)
		}
	}

	return w.Flush()
}

func getOSVersions(distro string, _ bool) error {
	releases, err := config.ListReleaseDetails(distrosRoot, distro)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tDISTRO\tOS_VERSION\tOS_RELEASE")

	for _, rel := range releases {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", rel.Name, distro, rel.OSVersion, rel.OSRelease)
	}

	return w.Flush()
}
