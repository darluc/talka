package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"talka/internal/llm"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "smokeollama: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fixture := ""
	baseURL := ""
	model := ""
	for index := 0; index < len(args); index++ {
		switch args[index] {
		case "--fixture":
			index++
			if index >= len(args) {
				return errors.New("missing value after --fixture")
			}
			fixture = args[index]
		case "--base-url":
			index++
			if index >= len(args) {
				return errors.New("missing value after --base-url")
			}
			baseURL = args[index]
		case "--model":
			index++
			if index >= len(args) {
				return errors.New("missing value after --model")
			}
			model = args[index]
		default:
			return fmt.Errorf("unknown argument %q", args[index])
		}
	}
	if fixture == "" {
		return errors.New("usage: smokeollama --fixture <path> [--base-url http://localhost:11434] [--model qwen3:8b]")
	}

	bytes, err := os.ReadFile(fixture)
	if err != nil {
		return err
	}

	provider := llm.NewOllamaProvider(llm.OllamaConfig{BaseURL: baseURL, Model: model})
	cleaned, err := provider.CleanupStrict(context.Background(), strings.TrimSpace(string(bytes)))
	if err != nil {
		return err
	}
	_, err = fmt.Println(cleaned)
	return err
}
