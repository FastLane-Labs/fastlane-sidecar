package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func main() {
	containerID := os.Getenv("CONTAINER_ID")
	if containerID == "" {
		fmt.Println("Please set CONTAINER_ID environment variable")
		os.Exit(1)
	}

	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
		Tail:       "10",
	}

	out, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		panic(err)
	}
	defer out.Close()

	fmt.Printf("Streaming logs from container %s...\n", containerID)
	streamLogs(out)
}

func streamLogs(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error reading log stream: %v\n", err)
	}
}
