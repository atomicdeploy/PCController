package windx

import (
	"errors"
	"image"
	"testing"
)

func TestBackendVocabulary(t *testing.T) {
	t.Parallel()
	for _, backend := range []Backend{
		BackendAuto,
		BackendDirectX,
		BackendLegacy,
	} {
		if !backend.Valid() {
			t.Fatalf("%q is not a valid backend", backend)
		}
		if backend.String() != string(backend) {
			t.Fatalf("String(%q) = %q", backend, backend.String())
		}
	}
	if Backend("warp-speed").Valid() {
		t.Fatal("unknown backend was accepted")
	}
}

func TestFeatureLevelStrings(t *testing.T) {
	t.Parallel()
	for level, want := range map[FeatureLevel]string{
		FeatureLevel9_1:  "9.1",
		FeatureLevel9_3:  "9.3",
		FeatureLevel10_0: "10.0",
		FeatureLevel10_1: "10.1",
		FeatureLevel11_0: "11.0",
		FeatureLevel11_1: "11.1",
		FeatureLevel12_0: "12.0",
		FeatureLevel12_1: "12.1",
	} {
		if got := level.String(); got != want {
			t.Fatalf("FeatureLevel(%#x).String() = %q, want %q", level, got, want)
		}
	}
}

func TestConvertPremultipliedRGBAToBGRAHonorsStride(t *testing.T) {
	t.Parallel()
	parent := image.NewRGBA(image.Rect(0, 0, 4, 3))
	frame := parent.SubImage(image.Rect(1, 1, 3, 3)).(*image.RGBA)
	values := [][4]byte{
		{10, 20, 30, 40},
		{50, 60, 70, 80},
		{90, 100, 110, 120},
		{130, 140, 150, 160},
	}
	index := 0
	for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
		for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
			offset := frame.PixOffset(x, y)
			copy(frame.Pix[offset:offset+4], values[index][:])
			index++
		}
	}

	destination := make([]byte, 2*2*4)
	if err := convertPremultipliedRGBA(frame, destination, 2, 2); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		30, 20, 10, 40,
		70, 60, 50, 80,
		110, 100, 90, 120,
		150, 140, 130, 160,
	}
	if !equalBytes(destination, want) {
		t.Fatalf("BGRA = %v, want %v", destination, want)
	}
}

func TestConvertPremultipliedRGBARejectsWrongSize(t *testing.T) {
	t.Parallel()
	frame := image.NewRGBA(image.Rect(0, 0, 2, 2))
	err := convertPremultipliedRGBA(frame, make([]byte, 16), 4, 1)
	if !errors.Is(err, ErrFrameSize) {
		t.Fatalf("error = %v, want ErrFrameSize", err)
	}
}

func TestResizeFrameBufferReusesGrowthAndReleasesLargeShrink(t *testing.T) {
	buffer, err := resizeFrameBuffer(nil, 40, 40)
	if err != nil {
		t.Fatal(err)
	}
	base := &buffer[0]
	for i := range buffer {
		buffer[i] = 255
	}
	buffer, err = resizeFrameBuffer(buffer, 41, 41)
	if err != nil || &buffer[0] != base || len(buffer) != 41*41*4 {
		t.Fatal("small resize did not reuse its backing buffer")
	}
	for _, value := range buffer {
		if value != 0 {
			t.Fatal("old frame pixels survived resize")
		}
	}
	buffer, err = resizeFrameBuffer(buffer, 10, 10)
	if err != nil || cap(buffer) > 2*len(buffer) {
		t.Fatal("large shrink retained peak-size GPU staging memory")
	}
}

func TestApplyPremultipliedOpacity(t *testing.T) {
	t.Parallel()
	source := []byte{20, 40, 60, 80, 100, 120, 140, 160}
	destination := make([]byte, len(source))
	applyPremultipliedOpacity(source, destination, 128)
	want := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	if !equalBytes(destination, want) {
		t.Fatalf("half-opacity BGRA = %v, want %v", destination, want)
	}
	applyPremultipliedOpacity(source, destination, 255)
	if !equalBytes(destination, source) {
		t.Fatalf("full-opacity BGRA = %v, want %v", destination, source)
	}
}

func TestDeviceLossClassification(t *testing.T) {
	t.Parallel()
	for _, code := range []uint32{
		hresultDXGIDeviceRemoved,
		hresultDXGIDeviceHung,
		hresultDXGIDeviceReset,
		hresultDXGIDriverInternalError,
	} {
		failure := &HRESULTError{
			Operation: "Present",
			Code:      code,
		}
		if !IsDeviceLost(failure) {
			t.Fatalf("HRESULT %#x was not classified as device loss", code)
		}
	}
	ordinary := &HRESULTError{
		Operation: "CreateSwapChainForComposition",
		Code:      hresultInvalidArgument,
	}
	if IsDeviceLost(ordinary) {
		t.Fatal("E_INVALIDARG was classified as device loss")
	}
	withReason := &HRESULTError{
		Operation:    "Commit",
		Code:         hresultInvalidArgument,
		DeviceReason: hresultDXGIDeviceRemoved,
	}
	if !IsDeviceLost(withReason) {
		t.Fatal("device-removed reason was ignored")
	}
}

func equalBytes(first, second []byte) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}
