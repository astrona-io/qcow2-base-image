package cloudinit

import (
	"testing"
	"time"
)

func TestSubstituteAllowlistOnly(t *testing.T) {
	tmpl := "key: ${SSH_KEY}\nscript: echo ${UNRELATED_VAR} $HOME\n"
	got := Substitute(tmpl, map[string]string{"SSH_KEY": "ssh-ed25519 AAAA"})

	want := "key: ssh-ed25519 AAAA\nscript: echo ${UNRELATED_VAR} $HOME\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSubstituteNoMatchLeavesUnchanged(t *testing.T) {
	tmpl := "no vars here"
	if got := Substitute(tmpl, map[string]string{"SSH_KEY": "x"}); got != tmpl {
		t.Errorf("got %q, want unchanged %q", got, tmpl)
	}
}

func TestRenderMetaDataReplacesInstanceID(t *testing.T) {
	in := "instance-id: ubuntu-24-04-desktop-base\nlocal-hostname: ubuntu-desktop\n"

	out, err := RenderMetaData(in, "ubuntu-24.04-base-1234567890")
	if err != nil {
		t.Fatalf("RenderMetaData: %v", err)
	}

	want := "instance-id: ubuntu-24.04-base-1234567890\nlocal-hostname: ubuntu-desktop\n"
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestRenderMetaDataMissingLineErrors(t *testing.T) {
	if _, err := RenderMetaData("local-hostname: foo\n", "x-1"); err == nil {
		t.Error("expected error when instance-id line is missing")
	}
}

func TestGenerateInstanceID(t *testing.T) {
	fixed := func() time.Time { return time.Unix(1700000000, 0) }
	got := GenerateInstanceID("ubuntu-24.04-base", fixed)

	want := "ubuntu-24.04-base-1700000000"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
