package storage

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

func runFFmpegOnFile(args []string, path string) ([]byte, error) {

	var cmd *exec.Cmd
	var err error
	if _, err = exec.LookPath("nsjail"); err == nil {
		cmd, err = sandboxFFmpegCommand(path, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to create sandbox command: %v", err)
		}
	} else {
		cmd = exec.Command("ffmpeg", args...)
	}

	var outputBuffer bytes.Buffer
	cmd.Stdout = &outputBuffer

	var errorBuffer bytes.Buffer
	cmd.Stderr = &errorBuffer

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg error: %v, stderr: %s", err, errorBuffer.String())
	}

	return outputBuffer.Bytes(), nil
}

func runFFmpegOnStream(args []string, stream io.Reader) ([]byte, error) {

	var cmd *exec.Cmd
	var err error
	if _, err = exec.LookPath("nsjail"); err == nil {
		cmd, err = sandboxFFmpegCommand("", args...)
		if err != nil {
			return nil, fmt.Errorf("failed to create sandbox command: %v", err)
		}
	} else {
		cmd = exec.Command("ffmpeg", args...)
	}

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

	io.Copy(stdinPipe, stream)

	stdinPipe.Close()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("%s: %s", err, errorBuffer.String())
	}

	return outputBuffer.Bytes(), nil
}

func runFFprobeOnStream(args []string, stream io.Reader) (string, error) {
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

	io.Copy(stdinPipe, stream)
	stdinPipe.Close()

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("%s: %s", err, errorBuffer.String())
	}

	return outputBuffer.String(), nil
}
