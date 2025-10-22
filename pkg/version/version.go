package version

// Version is the current version of the sidecar
// This can be set at build time using:
// go build -ldflags "-X github.com/FastLane-Labs/fastlane-sidecar/pkg/version.Version=x.y.z"
var Version = "dev"
