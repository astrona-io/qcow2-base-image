# Builds/installs the astroimg CLI. All pipeline logic and commands live in
# astroimg itself (cmd/astroimg) -- see README.md for usage
# (`astroimg pipeline --distro ubuntu`, etc). This Justfile only gets the
# binary in place and runs dev tooling.

binary := "bin/astroimg"

# Build the CLI into ./bin/astroimg
build:
    go build -o {{binary}} ./cmd/astroimg

# Install the CLI to ~/.local/bin -- no sudo needed. Make sure that's on your
# PATH (default on most Linux distros; add it yourself on macOS if needed).
install: build
    mkdir -p "$HOME/.local/bin"
    install -m 0755 {{binary}} "$HOME/.local/bin/astroimg"

# Run the Go unit test suite
test:
    go test ./...

# Run go vet + golangci-lint
lint:
    go vet ./...
    golangci-lint run
