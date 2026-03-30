package config

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/chzyer/readline"
)

// RunInteractive enters the config sub-CLI. Returns the updated config.
func RunInteractive(cfg *Config) *Config {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          "config> ",
		InterruptPrompt: "^C",
		EOFPrompt:       "back",
	})
	if err != nil {
		fmt.Printf("Failed to start config CLI: %v\n", err)
		return cfg
	}
	defer rl.Close()

	showParams(cfg)
	fmt.Println("\nType a number to edit, 'save' to save, 'back' to return.")

	for {
		line, err := rl.Readline()
		if err == readline.ErrInterrupt || err == io.EOF {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch strings.ToLower(line) {
		case "back", "exit", "quit":
			return cfg
		case "save":
			if err := cfg.Save(); err != nil {
				fmt.Printf("Failed to save: %v\n", err)
			} else {
				fmt.Printf("Config saved to %s\n", configPath())
			}
			continue
		case "help":
			showParams(cfg)
			fmt.Println("\nType a number to edit, 'save' to save, 'back' to return.")
			continue
		}

		// Try to parse as parameter number
		num, err := strconv.Atoi(line)
		if err != nil {
			fmt.Println("Unknown command. Type a number to edit, 'save', or 'back'.")
			continue
		}

		params := cfg.Parameters()
		if num < 1 || num > len(params) {
			fmt.Printf("Invalid number. Choose 1-%d.\n", len(params))
			continue
		}

		param := params[num-1]
		fmt.Printf("Current %s = %s\n", param.Name, param.Value)
		fmt.Printf("  %s\n", param.Description)
		rl.SetPrompt(fmt.Sprintf("%s> ", param.Name))
		newVal, err := rl.Readline()
		rl.SetPrompt("config> ")
		if err != nil {
			continue
		}
		newVal = strings.TrimSpace(newVal)
		if newVal == "" {
			fmt.Println("Keeping current value.")
			continue
		}

		if err := param.Set(newVal); err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Set %s = %s (type 'save' to persist)\n", param.Name, newVal)
			showParams(cfg)
		}
	}
	return cfg
}

func showParams(cfg *Config) {
	params := cfg.Parameters()
	fmt.Println("\n  Configuration:")
	for i, p := range params {
		fmt.Printf("  %d. %-20s = %-15s  (%s)\n", i+1, p.Name, p.Value, p.Description)
	}
}
