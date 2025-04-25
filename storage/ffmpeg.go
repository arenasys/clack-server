package storage

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

func runFFmpeg(args []string) ([]byte, error) {
	cmd := exec.Command("ffmpeg", args...)

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer

	var errorBuffer bytes.Buffer
	cmd.Stderr = &errorBuffer

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg error: %v, stderr: %s", err, errorBuffer.String())
	}

	return outputBuffer.Bytes(), nil
}

func runFFmpegOnReader(args []string, reader io.Reader) ([]byte, error) {
	cmd := exec.Command("ffmpeg", args...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer

	var errorBuffer bytes.Buffer
	cmd.Stderr = &errorBuffer

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %s", err, errorBuffer.String())
	}

	io.Copy(stdinPipe, reader)

	stdinPipe.Close()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("%s: %s", err, errorBuffer.String())
	}

	return outputBuffer.Bytes(), nil
}

func runFFprobeOnReader(args []string, reader io.Reader) (string, error) {
	cmd := exec.Command("ffprobe", args...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer

	var errorBuffer bytes.Buffer
	cmd.Stderr = &errorBuffer

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("%s: %s", err, errorBuffer.String())
	}

	io.Copy(stdinPipe, reader)
	stdinPipe.Close()

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("%s: %s", err, errorBuffer.String())
	}

	return outputBuffer.String(), nil
}
