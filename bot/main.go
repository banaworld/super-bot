package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gocolly/colly/v2"
	_ "github.com/lib/pq"           // PostgreSQL driver for Supabase
	_ "github.com/mattn/go-sqlite3" // For local WhatsApp session storage
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	// 1. Get Supabase connection string from environment
	connStr := os.Getenv("SUPABASE_DB_URL")
	if connStr == "" {
		fmt.Println("❌ Critical Error: SUPABASE_DB_URL secret is not set.")
		return
	}

	// SSL Fix for Supabase direct connections
	if !strings.Contains(connStr, "sslmode=") {
		if strings.Contains(connStr, "?") {
			connStr += "&sslmode=require"
		} else {
			connStr += "?sslmode=require"
		}
	}

	// 2. Connect to Supabase
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		fmt.Printf("❌ Failed to open Supabase connection: %v\n", err)
		return
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		fmt.Printf("❌ Cannot reach Supabase: %v\n", err)
		return
	}
	fmt.Println("✅ Successfully connected to Supabase.")

	// 3. Start components
	go startScraper(db)
	startWhatsApp(db)
}

func saveToSupabase(db *sql.DB, source, content string) {
	// Updated query for your property leads logic
	query := `INSERT INTO scraped_data (name, url, created_at, updated_at) 
              VALUES ($1, $2, NOW(), NOW())`
	_, err := db.Exec(query, source, content)
	if err != nil {
		fmt.Printf("❌ Supabase Save Error: %v\n", err)
	} else {
		fmt.Printf("🚀 Saved to Cloud: [%s]\n", source)
	}
}

func startScraper(db *sql.DB) {
	c := colly.NewCollector(colly.Async(true))
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		name := e.Text
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if name != "" && link != "" {
			saveToSupabase(db, "Web: "+name, link)
		}
	})

	for {
		fmt.Println("🌐 [Scraper] Starting crawl...")
		c.Visit("https://example.com") // Replace with your property search URLs
		c.Wait()
		time.Sleep(30 * time.Minute)
	}
}

func startWhatsApp(db *sql.DB) {
	// Session storage for Hugging Face persistence
	sessionPath := "file:/app/bot/data/wa_session.db?_foreign_keys=on"
	container, err := sqlstore.New("sqlite3", sessionPath, waLog.Noop)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice()
	if err != nil {
		panic(err)
	}

	client := whatsmeow.NewClient(deviceStore, waLog.Stdout("Client", "WARN", true))

	client.AddEventHandler(func(evt interface{}) {
		if v, ok := evt.(*events.Message); ok {
			msgText := v.Message.GetConversation()
			if msgText == "" {
				msgText = v.Message.GetExtendedTextMessage().GetText()
			}
			sender := v.Info.Sender.ToNonAD().String()

			if msgText != "" {
				fmt.Printf("📩 [WhatsApp] Lead from %s\n", sender)
				saveToSupabase(db, "WA: "+sender, msgText)
			}
		}
	})

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\n📸 [WhatsApp] SCAN QR IN LOGS:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		fmt.Println("✅ [WhatsApp] Connected...")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	client.Disconnect()
}
