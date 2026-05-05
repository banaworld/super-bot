package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gocolly/colly/v2"
	_ "github.com/lib/pq" // Supabase/Postgres Driver
	_ "github.com/mattn/go-sqlite3"
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	// 1. Supabase Connection String
	// Set this in Hugging Face Secrets as SUPABASE_DB_URL
	// Format: postgres://postgres:[password]@[host]:5432/postgres
	connStr := os.Getenv("SUPABASE_DB_URL")
	if connStr == "" {
		fmt.Println("❌ Error: SUPABASE_DB_URL environment variable is not set")
		return
	}

	// 2. Open Supabase Connection
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		fmt.Printf("❌ Failed to connect to Supabase: %v\n", err)
		return
	}
	defer db.Close()

	// 3. Start Background Scraper
	go startScraper(db)

	// 4. Start WhatsApp Bot
	startWhatsApp(db)
}

// --- DATABASE HELPER ---
func saveToSupabase(db *sql.DB, source, content string) {
	// Postgres uses $1, $2 placeholders
	query := `INSERT INTO scraped_data (name, url, created_at, updated_at) 
              VALUES ($1, $2, NOW(), NOW())`
	
	_, err := db.Exec(query, source, content)
	if err != nil {
		fmt.Printf("❌ Supabase Insert Error: %v\n", err)
	} else {
		fmt.Printf("✅ Saved to Supabase: [%s] %s\n", source, content)
	}
}

// --- WEB SCRAPER ---
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
		c.Visit("https://example.com") // Replace with your target
		c.Wait()
		fmt.Println("🌐 [Scraper] Sleeping for 30 minutes...")
		time.Sleep(30 * time.Minute)
	}
}

// --- WHATSAPP BOT ---
func startWhatsApp(db *sql.DB) {
	// WhatsApp session storage stays local to the container
	// (Prevents needing to scan QR every time the Space restarts)
	sessionPath := "file:/app/bot/data/wa_session.db?_foreign_keys=on"
	container, err := sqlstore.New("sqlite3", sessionPath, waLog.Noop)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice()
	client := whatsmeow.NewClient(deviceStore, waLog.Noop)

	client.AddEventHandler(func(evt interface{}) {
		if v, ok := evt.(*events.Message); ok {
			msgText := v.Message.GetConversation()
			if msgText == "" {
				msgText = v.Message.GetExtendedTextMessage().GetText()
			}
			sender := v.Info.Sender.ToNonAD().String()

			if msgText != "" {
				fmt.Printf("📩 [WhatsApp] Received from %s\n", sender)
				saveToSupabase(db, "WA: "+sender, msgText)
			}
		}
	})

	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		_ = client.Connect()
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\n📸 [WhatsApp] SCAN THIS QR CODE IN YOUR LOGS:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			}
		}
	} else {
		_ = client.Connect()
		fmt.Println("✅ [WhatsApp] Logged in successfully!")
	}

	// Stop handling for Supervisor
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	client.Disconnect()
}
