package main

import (
	"fmt"
	"log"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	telegram_token := os.Getenv("TELEGRAM_API_TOKEN")
	bot, err := tgbotapi.NewBotAPI(telegram_token)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	type gambler_element struct {
		userid int64
		gambles int
		gamble_hour time.Time
	}
	var gamblers = make(map[int64]*gambler_element) 
	for update := range updates {
		if update.Message.Dice != nil { 
			if update.Message.ForwardFrom == nil {
				if update.Message.Dice.Emoji == "🎰" {
					userid := update.Message.From.ID
					gambler, ok := gamblers[userid]
					if !ok {
						gambler = &gambler_element{userid: userid, gambles: 0, gamble_hour: time.Now()}
						gamblers[userid] = gambler
					}
					if time.Since(gambler.gamble_hour).Minutes() < 60 {
						gambler.gambles++
					} else {
						gambler.gambles = 1
						gambler.gamble_hour = time.Now()
					}
					if gambler.gambles > 3 {
						msg_text := fmt.Sprintf("@%s ЛИМИТ ГАМБЫ ПРЕВЫШЕН!\nПопробуйте снова через %.0f минут!\n", update.Message.From.UserName, 60 - time.Since(gambler.gamble_hour).Minutes())
						msg := tgbotapi.NewMessage(update.Message.Chat.ID, msg_text)
						del_gamba := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, update.Message.MessageID)
						bot.Send(del_gamba)
						msg_sent, err := bot.Send(msg)
						if err != nil {
							log.Printf("Error sending message: %v", err)
						}
						del_msg := tgbotapi.NewDeleteMessage(update.Message.Chat.ID, msg_sent.MessageID)
						go func() {
							time.Sleep(time.Duration(3 *time.Second))
							bot.Send(del_msg)
						}()
					}
				}
			}
		}
	}

}