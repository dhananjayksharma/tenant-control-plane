package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dhananjayksharma/tenant-control-plane/internal/broker"
	"github.com/dhananjayksharma/tenant-control-plane/internal/httpapi"
	"github.com/dhananjayksharma/tenant-control-plane/internal/store"
)

func env(key, def string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return def
}
func main() {
	log.SetPrefix("service=api ")
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)
	log.Print("starting API")
	ctx := context.Background()
	url := env("DATABASE_URL", "postgres://app:app@localhost:5432/tenants?sslmode=disable")
	var db *store.DB
	var e error
	for i := 0; i < 30; i++ {
		db, e = store.Open(url)
		if e == nil {
			break
		}
		log.Printf("postgres connection failed attempt=%d max_attempts=30 error=%v", i+1, e)
		time.Sleep(time.Second)
	}
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	log.Print("postgres connected; running migrations")
	if e = db.Migrate(ctx); e != nil {
		log.Fatal(e)
	}
	log.Print("database migrations complete")
	handler := httpapi.NewHandler(db)
	go broker.Run(ctx, db, env("AMQP_URL", "amqp://guest:guest@localhost:5672/"))
	log.Printf("API listening on %s", env("PORT", ":8080"))
	log.Fatal(http.ListenAndServe(env("PORT", ":8080"), handler))
}
