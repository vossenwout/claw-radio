package cmd

import (
	"errors"

	"github.com/vossenwout/claw-radio/internal/mpv"
)

const mpvNotRunningMessage = "radio is not running. Start with: claw-radio start"

type playbackClient interface {
	Close() error
	InsertNext(path string) error
}

var dialPlaybackClientFn = func(socketPath string) (playbackClient, error) {
	return mpv.Dial(socketPath)
}

func dialPlaybackClient(socketPath string) (playbackClient, error) {
	client, err := dialPlaybackClientFn(socketPath)
	if err == nil {
		return client, nil
	}
	return nil, exitCode(errors.New(mpvNotRunningMessage), 5)
}
