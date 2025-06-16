package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
)

func init() {
	var err error
	defer func() {
		if err != nil {
			log.Fatal(err)
		}
	}()

	eventDir := "./event"

	jsonFiles, err := os.ReadDir(eventDir)
	if err != nil {
		err = fmt.Errorf("error os.ReadDir. dir: %s, err: %v", eventDir, err)
		return
	}

	for _, jsonFile := range jsonFiles {
		filename := jsonFile.Name()

		filenames = append(filenames, filename)

		filepath := fmt.Sprintf("%s/%s", eventDir, filename)

		bytes, err := loadFile(filepath)
		if err != nil {
			return
		}

		if !json.Valid(bytes) {
			err = fmt.Errorf("error json invalid. filepath: %s", filepath)
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
			log.Fatalf("SSE server shutdown error. err: %v", err)
		}
	})

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error. err: %v", err)
		}
	}()

	messages := make(chan string)
	go func() {
		for msg := range messages {
			log.Println("broadcasting message: ", msg)
			sseMsg := &sse.Message{}
			sseMsg.AppendData(msg)
			if err := sseServer.Publish(sseMsg); err != nil {
				log.Println("SSE server publish error: ", err)
			}
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan bool, 1)

	go func() {
		fmt.Println("input: ")

		reader := bufio.NewReader(os.Stdin)
		for {
			input, err := reader.ReadString('\n')
			if err != nil {
				log.Println("error reader.ReadString. ", err)
				done <- true
				return
			}

			input = strings.ToLower(strings.TrimSpace(input))

			if input == "" {
				fmt.Println("input valid command")
				continue
			}

			command := strings.Split(input, " ")

			switch command[0] {
			case "exit":
				log.Println("Exiting...")
				done <- true
				return
			case "json":
				if len(command) < 2 {
					fmt.Println("input at least one valid json filename as argument")
					continue
				}

				jsonInputs := make([]string, 0)
				invalidFilenames := make([]string, 0)

				for i := 1; i < len(command); i++ {
					filename := command[i]

					jsonStr, exists := filenameToJSONStr[filename]
					if !exists {
						invalidFilenames = append(invalidFilenames, filename)
						continue
					}

					jsonInputs = append(jsonInputs, jsonStr)
				}

				if len(invalidFilenames) > 0 {
					fmt.Printf("files %v not exists. available files: %s\n", invalidFilenames, filenames)
					continue
				}

				for _, jsonStr := range jsonInputs {
					messages <- jsonStr
				}
			default:
				messages <- input
			}
		}
	}()

	select {
	case <-sigChan:
	case <-done:
	}

	log.Println("graceful shutdown")

	close(messages)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(ctx); err != nil {
		log.Fatalf("server shutdown error. %v", err)
	}

	log.Println("exited")
}

func loadFile(filepath string) ([]byte, error) {
	file, err := os.Open(filepath)
	if err != nil {
		err = fmt.Errorf("error os.Open. filepath: %s, err: %v", filepath, err)
		return nil, err
	}
	defer file.Close()

	bytes, err := io.ReadAll(file)
	if err != nil {
		err = fmt.Errorf("error io.ReadAll. filepath: %s, err: %v", filepath, err)
		return nil, err
	}

	return bytes, nil
}
