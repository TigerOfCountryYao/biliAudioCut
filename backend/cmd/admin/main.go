package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/config"
	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/identity"
	"github.com/TigerOfCountryYao/biliAudioCut/backend/internal/platform/database"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "create" {
		log.Fatal("usage: go run ./cmd/admin create --email <email> --name <name>")
	}

	createAdmin(os.Args[2:])
}

func createAdmin(args []string) {
	flags := flag.NewFlagSet("create", flag.ExitOnError)

	var email string
	var name string

	flags.StringVar(&email, "email", "", "administrator email address")
	flags.StringVar(&name, "name", "", "administrator display name")
	_ = flags.Parse(args)

	password := readPassword()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	service := identity.NewService(db)
	user, err := service.BootstrapAdmin(context.Background(), identity.BootstrapAdminInput{
		Email:       email,
		DisplayName: name,
		Password:    password,
	})
	if errors.Is(err, identity.ErrAlreadyInitialized) {
		log.Fatal("system is already initialized; use the future invitation flow to add members")
	}
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("administrator created: %s <%s>\n", user.DisplayName, user.Email)
}

func readPassword() string {
	fmt.Print("Password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()

	if err != nil {
		log.Fatal(err)
	}

	fmt.Print("Confirm password: ")
	confirmation, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()

	if err != nil {
		log.Fatal(err)
	}

	if string(password) != string(confirmation) {
		log.Fatal("password confirmation does not match")
	}

	return string(password)
}
