package platform

import "testing"

func TestValidateArch(t *testing.T) {
	if err := ValidateArch("arm64"); err != nil {
		t.Errorf("arm64 should be valid: %v", err)
	}

	if err := ValidateArch("amd64"); err != nil {
		t.Errorf("amd64 should be valid: %v", err)
	}

	if err := ValidateArch("mips"); err == nil {
		t.Error("mips should be rejected")
	}
}

func TestBinaryAndMachine(t *testing.T) {
	bin, machine, err := BinaryAndMachine("arm64")
	if err != nil || bin != "qemu-system-aarch64" || machine != "virt,highmem=on" {
		t.Errorf("arm64: got (%q, %q, %v)", bin, machine, err)
	}

	bin, machine, err = BinaryAndMachine("amd64")
	if err != nil || bin != "qemu-system-x86_64" || machine != "q35" {
		t.Errorf("amd64: got (%q, %q, %v)", bin, machine, err)
	}
}

func TestSelectAccelNativeDarwin(t *testing.T) {
	got := SelectAccel("darwin", "arm64", "arm64")
	want := []string{"-accel", "hvf", "-cpu", "host"}
	assertEqualSlice(t, got, want)
}

func TestSelectAccelNativeLinux(t *testing.T) {
	got := SelectAccel("linux", "amd64", "amd64")
	want := []string{"-accel", "kvm", "-cpu", "host"}
	assertEqualSlice(t, got, want)
}

func TestSelectAccelCrossArch(t *testing.T) {
	got := SelectAccel("darwin", "arm64", "amd64")
	want := []string{"-cpu", "qemu64"}
	assertEqualSlice(t, got, want)

	got = SelectAccel("linux", "amd64", "arm64")
	want = []string{"-cpu", "cortex-a57"}
	assertEqualSlice(t, got, want)
}

func TestHeadlessExplicitWins(t *testing.T) {
	yes := true
	no := false

	if !Headless(&yes) {
		t.Error("explicit true should force headless")
	}

	if Headless(&no) {
		t.Error("explicit false should force non-headless")
	}
}

func TestDisplayArgsHeadless(t *testing.T) {
	got := DisplayArgs(true, "build/console.log")
	want := []string{"-display", "none", "-serial", "file:build/console.log"}
	assertEqualSlice(t, got, want)
}

func TestDisplayArgsGUI(t *testing.T) {
	got := DisplayArgs(false, "")
	want := []string{"-display", "cocoa,show-cursor=on,zoom-to-fit=on"}
	assertEqualSlice(t, got, want)
}

func TestFindEFINoCandidatesError(t *testing.T) {
	_, _, err := FindEFI("plan9", "arm64")
	if err == nil {
		t.Error("expected error for unknown host OS with no candidates")
	}
}

func assertEqualSlice(t *testing.T, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
