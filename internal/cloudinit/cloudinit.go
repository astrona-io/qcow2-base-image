// Package cloudinit renders the user-data/meta-data templates staged into
// distros/<name>/ or layers/<name>/. It replaces two pieces of the old
// shell pipeline: `envsubst '${SSH_KEY}'` (allowlisted substitution, so a
// literal "${...}" inside a runcmd bash snippet is never touched) and the
// per-build unique instance-id `sed` line.
package cloudinit

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var varPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Substitute replaces only the "${KEY}" occurrences whose KEY is present in
// vars, leaving every other "${...}" in the template untouched. This is the
// same restricted behavior as `envsubst '${SSH_KEY}'` in the old
// prepare.sh, deliberately narrower than full envsubst/shell expansion so a
// user-data template can't accidentally have unrelated bash variable
// references mangled.
func Substitute(template string, vars map[string]string) string {
	return varPattern.ReplaceAllStringFunc(template, func(match string) string {
		key := varPattern.FindStringSubmatch(match)[1]
		if v, ok := vars[key]; ok {
			return v
		}

		return match
	})
}

var instanceIDPattern = regexp.MustCompile(`(?m)^instance-id:.*$`)

// RenderMetaData rewrites the "instance-id:" line in a meta-data template to
// a freshly generated, unique instance-id. cloud-init only runs
// per-instance modules (users, ssh_authorized_keys, packages, power_state,
// ...) once per instance-id, so reusing a static id causes cloud-init to
// silently skip re-provisioning on a disk that already booted once before
// -- this matters even more for layers, which boot an already-sysprepped
// base disk and rely on a fresh instance-id to force cloud-init to run
// again.
func RenderMetaData(metaData, instanceID string) (string, error) {
	if !instanceIDPattern.MatchString(metaData) {
		return "", errors.New("meta-data template has no \"instance-id:\" line")
	}

	return instanceIDPattern.ReplaceAllLiteralString(metaData, "instance-id: "+instanceID), nil
}

// GenerateInstanceID builds a unique instance-id from a prefix and a clock
// (inject time.Now in production, a fixed func in tests).
func GenerateInstanceID(prefix string, now func() time.Time) string {
	return fmt.Sprintf("%s-%d", prefix, now().Unix())
}
