//go:build dev

// Dev-only rpath additions. With `-tags dev` the linker also stamps
// the absolute path to the runtime's NativeAOT publish directory into
// the binary's rpath, so local builds find the runtime without copying
// it next to the executable. Release builds (no tag) skip this so
// shipped binaries don't leak the build-machine path.

package gafferruntime

/*
#cgo linux,amd64   LDFLAGS: -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-x64/publish
#cgo linux,arm64   LDFLAGS: -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/linux-arm64/publish
#cgo darwin,amd64  LDFLAGS: -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/osx-x64/publish
#cgo darwin,arm64  LDFLAGS: -Wl,-rpath,${SRCDIR}/../../runtime/Gaffer.Runtime/bin/Release/net10.0/osx-arm64/publish
*/
import "C"
