package sfxgen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestGenerateCatalogIsDeterministic(t *testing.T) {
	t.Parallel()
	expected := []struct {
		cue  string
		file string
		size int
		hash string
	}{
		{
			cue:  "on",
			file: "on.wav",
			size: 31_796,
			hash: "e22c40a97cd2bcc67c15fa8aad75d7b304093bda0014e1950b985998dcb34a92",
		},
		{
			cue:  "off",
			file: "off.wav",
			size: 29_150,
			hash: "40b4027ec40fb4068614f1a0dc6b17016c81bfd52170e8d61b0f524f2449f0a6",
		},
		{
			cue:  "message",
			file: "message.wav",
			size: 34_442,
			hash: "c08a0be2d1a5cb3c49d7c5bd264a30b118c6cd4012933699b103ad960538a136",
		},
		{
			cue:  "error",
			file: "error.wav",
			size: 38_852,
			hash: "c3068e971cb6899f552851330e7ed9a57e71e69ea03bced433afb7fb0c4948b5",
		},
	}
	first := GenerateCatalog()
	second := GenerateCatalog()
	if len(first) != len(expected) || len(second) != len(expected) {
		t.Fatalf(
			"catalog sizes = (%d, %d), want %d",
			len(first),
			len(second),
			len(expected),
		)
	}
	for index, want := range expected {
		got := first[index]
		if got.Cue != want.cue || got.FileName != want.file {
			t.Fatalf("asset %d metadata = %+v", index, got)
		}
		if !bytes.Equal(got.Data, second[index].Data) {
			t.Fatalf("%s changed between identical generations", got.Cue)
		}
		if len(got.Data) != want.size {
			t.Fatalf("%s size = %d, want %d", got.Cue, len(got.Data), want.size)
		}
		sum := sha256.Sum256(got.Data)
		if actual := hex.EncodeToString(sum[:]); actual != want.hash {
			t.Fatalf("%s hash = %s, want %s", got.Cue, actual, want.hash)
		}
		if !bytes.Equal(got.Data[0:4], []byte("RIFF")) ||
			!bytes.Equal(got.Data[8:12], []byte("WAVE")) {
			t.Fatalf("%s is not RIFF/WAVE", got.Cue)
		}
	}
}

func TestGenerateRejectsUnknownCue(t *testing.T) {
	t.Parallel()
	if data, err := Generate("missing"); err == nil || data != nil {
		t.Fatalf("Generate(missing) = (%d bytes, %v)", len(data), err)
	}
}
