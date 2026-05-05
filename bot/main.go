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
	_ "github.com/lib/pq"           // PostgreSQL driver for Supabase
	_ "github.com/mattn/go-sqlite3" // For local WhatsApp session storage
	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	// 1. Get Supabase connection string from Hugging Face Secrets
	connStr := os.Getenv("SUPABASE_DB_URL")
	if connStr == "" {
		fmt.Println("❌ Critical Error: SUPABASE_DB_URL secret is not set in Hugging Face.")
		return
	}

	// 2. Connect to Supabase
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		fmt.Printf("❌ Failed to open Supabase connection: %v\n", err)
		return
	}
	defer db.Close()

	// Verify connection
	if err := db.Ping(); err != nil {
		fmt.Printf("❌ Cannot reach Supabase: %v\n", err)
		return
	}
	fmt.Println("✅ Successfully connected to Supabase.")

	// 3. Start the Web Scraper in the background
	go startScraper(db)

	// 4. Start the WhatsApp Bot (This keeps the main thread alive)
	startWhatsApp(db)
}

// --- DATABASE HELPER ---
func saveToSupabase(db *sql.DB, source, content string) {
	// Supabase/Postgres uses $1, $2 placeholders instead of ?
	query := `INSERT INTO scraped_data (name, url, created_at, updated_at) 
              VALUES ($1, $2, NOW(), NOW())`
	
	_, err := db.Exec(query, source, content)
	if err != nil {
		fmt.Printf("❌ Supabase Save Error: %v\n", err)
	} else {
		fmt.Printf("🚀 Saved to Cloud: [%s] %s\n", source, content)
	}
}

// --- WEB SCRAPER COMPONENT ---
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
		fmt.Println("🌐 [Scraper] Starting new crawl cycle...")
		// You can change this URL to any site you want to monitor
		c.Visit("https://example.com") 
		c.Wait()
		time.Sleep(30 * time.Minute)
	}
}

// --- WHATSAPP BOT COMPONENT ---
func startWhatsApp(db *sql.DB) {
	// We use a local SQLite file JUST to store the WhatsApp login session
	// This prevents having to scan the QR code every time the container restarts
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

	// Handle incoming messages
	client.AddEventHandler(func(evt interface{}) {
		if v, ok := evt.(*events.Message); ok {
			msgText := v.Message.GetConversation()
			if msgText == "" {
				msgText = v.Message.GetExtendedTextMessage().GetText()
			}
			sender := v.Info.Sender.ToNonAD().String()

			if msgText != "" {
				fmt.Printf("📩 [WhatsApp] Message from %s\n", sender)
				saveToSupabase(db, "WA: "+sender, msgText)
			}
		}
	})

	// Login Logic
	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				fmt.Println("\n📸 [WhatsApp] SCAN THIS QR CODE IN YOUR LOGS:")
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		fmt.Println("✅ [WhatsApp] Connected and listening...")
	}

	// Graceful shutdown handling for Supervisor
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	client.Disconnect()
}
