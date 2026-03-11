package main

import "embed"

// ttsFS stores the bundled Python daemon and default voice reference files.
//
//go:embed tts
var ttsFS embed.FS
