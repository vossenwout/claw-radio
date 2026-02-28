package main

import "testing"

func TestEmbeddedTTSDaemonIsReadable(t *testing.T) {
	data, err := ttsFS.ReadFile("tts/daemon.py")
	if err != nil {
		t.Fatalf("ReadFile(\"tts/daemon.py\") error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("embedded tts/daemon.py is empty")
	}
}

func TestEmbeddedPopVoiceHasWAVHeader(t *testing.T) {
	data, err := ttsFS.ReadFile("tts/voices/pop.wav")
	if err != nil {
		t.Fatalf("ReadFile(\"tts/voices/pop.wav\") error = %v", err)
	}
	if len(data) < 4 {
		t.Fatalf("embedded WAV too short: %d bytes", len(data))
	}
	if string(data[:4]) != "RIFF" {
		t.Fatalf("WAV header = %q, want %q", string(data[:4]), "RIFF")
	}
}
