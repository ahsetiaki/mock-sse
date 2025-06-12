package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tmaxmax/go-sse"
)

var (
	filenames         = make([]string, 0)
	filenameToJSONStr = make(map[string]string)

	slogger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
)

func init() {
	var err error
	defer func() {
		if err != nil {
			os.Exit(1)
		}
	}()

	eventDir := "./event"

	jsonFiles, err := os.ReadDir(eventDir)
	if err != nil {
		slogger.Error("error os.ReadDir", "dir", eventDir, "err", err)
		return
	}

	for _, jsonFile := range jsonFiles {
		filename := jsonFile.Name()

		filenames = append(filenames, filename)

		filepath := fmt.Sprintf("%s/%s", eventDir, filename)

		file, err := os.Open(filepath)
		if err != nil {
			slogger.Error("error os.Open", "filepath", filepath, "err", err)
			return
		}
		defer file.Close()

		bytes, err := io.ReadAll(file)
		if err != nil {
			slogger.Error("error io.ReadAll", "filepath", filepath, "err", err)
			return
		}

		if !json.Valid(bytes) {
			slogger.Error("error json invalid", "filepath", filepath)
			err = fmt.Errorf("error json invalid %s", filepath)
			return
		}

		filenameToJSONStr[filename] = string(bytes)
	}
}

func main() {
	sseServer := &sse.Server{}

	mux := http.NewServeMux()
	mux.Handle("/events", sseServer)

	httpServer := &http.Server{
		Addr:    ":9999",
		Handler: mux,
	}

	httpServer.RegisterOnShutdown(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := sseServer.Shutdown(ctx); err != nil {
			slogger.Error("SSE server shutdown error", "err", err)
		}
	})

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slogger.Error("HTTP server error", "err", err)
		}
	}()

	messages := make(chan string)
	go func() {
		for msg := range messages {
			slogger.Info("Broadcasting message", "msg", msg)
			sseMsg := &sse.Message{}
			sseMsg.AppendData(msg)
			if err := sseServer.Publish(sseMsg); err != nil {
				slogger.Error("SSE server publish error", "err", err)
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan bool, 1)

	go func() {
		fmt.Println("Input message")

		reader := bufio.NewReader(os.Stdin)
		for {
			input, err := reader.ReadString('\n')
			if err != nil {
				slogger.Error("error reader.ReadString", "err", err)
				done <- true
				return
			}

			input = strings.TrimSpace(input)

			command := strings.Split(input, " ")

			if strings.EqualFold(input, "exit") {
				slogger.Info("Exiting...")
				done <- true
				return
			} else if len(command) > 1 {
				cmd := command[0]

				switch cmd {
				case "json":
					arg := command[1]

					jsonStr, exists := filenameToJSONStr[arg]

					if !exists {
						slogger.Error(fmt.Sprintf("file not %s exists. available file: %s", arg, filenames))
						continue
					}

					messages <- jsonStr
				}
			} else {
				messages <- input
			}
		}
	}()

	select {
	case <-sigChan:
	case <-done:
	}

	slogger.Info("Graceful shutdown")

	close(messages)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		slogger.Error("Server shutdown error", "err", err)
	}
}
