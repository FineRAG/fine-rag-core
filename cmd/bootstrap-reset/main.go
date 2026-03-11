package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

func hs(v string) string {
	s := sha256.Sum256([]byte(v))
	return "sha256$" + hex.EncodeToString(s[:])
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	user := os.Getenv("BOOTSTRAP_USER")
	pass := os.Getenv("BOOTSTRAP_PASS")
	api := os.Getenv("BOOTSTRAP_API")

	if dsn == "" || user == "" || pass == "" || api == "" {
		fmt.Fprintln(os.Stderr, "required env: DATABASE_URL, BOOTSTRAP_USER, BOOTSTRAP_PASS, BOOTSTRAP_API")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	res, err := db.Exec(
		"UPDATE app_users SET password_hash=$1, api_key_hash=$2, active=TRUE WHERE username=$3",
		hs(pass), hs(api), user,
	)
	if err != nil {
		panic(err)
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		_, err = db.Exec(
			"INSERT INTO app_users (username, password_hash, api_key_hash, active) VALUES ($1,$2,$3,TRUE)",
			user, hs(pass), hs(api),
		)
		if err != nil {
			panic(err)
		}
		fmt.Println("bootstrap user inserted")
	} else {
		fmt.Printf("bootstrap user updated (rows=%d)\n", n)
	}
}
