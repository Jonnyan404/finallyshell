package main

import (
	"os"
	"runtime"
)

// useMemoryRendering asks MyGo to draw frames in memory on macOS Intel,
// where a frame can arrive that never finishes.
//
// MyGo draws with Metal there (internal/gpu/metal) and waits for each frame on
// the UI thread, waitUntilCompleted. Where the GPU's driver had to be patched
// to run a macOS the model does not support — an OpenCore Legacy Patcher
// install, say — the watchdog can kill that command buffer instead
// (kIOAccelCommandBufferCallbackErrorTimeout): the frame blocks for the whole
// timeout with the window painted nothing, and render() hands the driver
// another frame in the same frame, so the window stays blank and compositing
// stalls for every other app too (egoist/mygo#1503).
//
// Drawing in memory never touches the GPU and costs CPU time per frame
// instead (9–50 ms on a Haswell Iris Pro). An explicit MYGO_GPU in the
// environment still decides, so a machine whose driver is fine can ask for the
// GPU back with MYGO_GPU=1.
//
// TODO(egoist/mygo#1505): remove this default once that change ships. It makes
// MyGo draw a frame that timed out in memory and retry the GPU later, so the
// failure heals itself per frame and Intel Macs no longer need to give up the
// GPU wholesale — which here costs CPU time on every Intel Mac whose driver was
// never patched, not just on the OCLP ones. The workaround is this function and
// memoryRendering below; drop both, and TestMemoryRendering with them.
func useMemoryRendering() {
	if memoryRendering(runtime.GOOS, runtime.GOARCH, os.Getenv("MYGO_GPU")) {
		os.Setenv("MYGO_GPU", "0")
	}
}

// memoryRendering reports whether MyGo should draw in memory: on macOS Intel,
// unless the environment already says which way.
func memoryRendering(gos, goarch, mygoGPU string) bool {
	return gos == "darwin" && goarch == "amd64" && mygoGPU == ""
}
