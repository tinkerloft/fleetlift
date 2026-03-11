package main

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}

	cmd.AddCommand(authLoginCmd())
	cmd.AddCommand(authStatusCmd())
	cmd.AddCommand(authLogoutCmd())

	return cmd
}

func authLoginCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log in via GitHub OAuth",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := serverURL + "/auth/github"
			fmt.Printf("Opening browser to: %s\n", url)
			fmt.Println("After authenticating, paste the token here.")

			if err := openBrowser(url); err != nil {
				fmt.Printf("Could not open browser. Visit the URL manually.\n")
			}

			fmt.Print("\nToken: ")
			var token string
			if _, err := fmt.Scanln(&token); err != nil {
				return fmt.Errorf("read token: %w", err)
			}

			if err := saveToken(token); err != nil {
				return fmt.Errorf("save token: %w", err)
			}

			fmt.Println("Logged in successfully. Token saved to ~/.fleetlift/auth.json")
			return nil
		},
	}
}

func authStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current authentication status",
		RunE: func(cmd *cobra.Command, args []string) error {
			token := loadToken()
			if token == "" {
				fmt.Println("Not logged in. Run: fleetlift auth login")
				return nil
			}
			fmt.Println("Authenticated. Token stored in ~/.fleetlift/auth.json")
			return nil
		},
	}
}

func authLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove saved credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := saveToken(""); err != nil {
				return err
			}
			fmt.Println("Logged out.")
			return nil
		},
	}
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("open", url)
	}
	return cmd.Start()
}
