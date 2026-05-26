package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

// Gambler represents a user who has gambled
//
// Gambles - the number of times the user has gambled (resets every 60 minutes)
// GambleTime - the time when the user last gambled
// Username - the username of the user
// Wins - the number of times the user has won
// AllGambles - the total number of gambles the user has made
type Gambler struct {
	UserID      int64
	Gambles     int
	GambleTime  int64
	Username    string
	Wins        int
	AllGambles  int
	NotifyTimer bool
	Notified    bool
}

func main() {
	// Load .env file
	err := godotenv.Load()
	handleError(err)

	// Create bot
	telegramToken := os.Getenv("TELEGRAM_API_TOKEN")
	adminUsername := os.Getenv("ADMIN_USERNAME")
	bot, err := tgbotapi.NewBotAPI(telegramToken)
	handleError(err)

	log.Printf("Authorized on account %s", bot.Self.UserName)

	err = initDB()
	handleError(err)

	go func() {
		for {
			time.Sleep(time.Second * 20)
			gamblers, err := loadGamblerData()
			handleError(err)
			for _, gambler := range gamblers {
				if gambler.NotifyTimer {
					sinceGamble := time.Since(time.Unix(gambler.GambleTime, 0))
					if sinceGamble.Minutes() > 60 && !gambler.Notified {
						err = notify(bot, gambler)
						if err != nil {
							log.Printf("Can't send message to %s", gambler.Username)
						} else {
							gambler.Notified = true
						}
						saveGamblerData(gamblers, 0, "")
					}
				}
			}

		}
	}()

	// Create update channel
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	// Start listening for updates
	for update := range updates {
		// Skip if the update doesn't contain a message
		if update.Message == nil {
			continue
		}

		if update.Message.Text == "/top" {
			gamblers, err := loadGamblerData()
			handleError(err)
			topText := getTopGamblers(gamblers, bot, update.Message.Chat.ID)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, topText)
			msg.DisableNotification = true
			bot.Send(msg)
		}

		if update.Message.Text == "/pulls" {
			if update.Message.From.UserName == adminUsername {
				pullStats, err := loadPullStats()
				handleError(err)
				var entries []string
				for symbol, count := range pullStats {
					entries = append(entries, fmt.Sprintf("%s: %d", symbol, count))
				}
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, strings.Join(entries, "\n"))
				msg.DisableNotification = true
				bot.Send(msg)
			} else {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Ты не величайший админ")
				msg.DisableNotification = true
				bot.Send(msg)
			}
		}

		if update.Message.Text == "/notify" {
			gamblers, err := loadGamblerData()
			handleError(err)

			gambler, ok := gamblers[update.Message.From.ID]
			if !ok {
				gambler = &Gambler{
					UserID:      update.Message.From.ID,
					Gambles:     0,
					GambleTime:  time.Now().Unix(),
					Username:    update.Message.From.UserName,
					Wins:        0,
					AllGambles:  0,
					NotifyTimer: false,
					Notified:    false,
				}
				gamblers[update.Message.From.ID] = gambler
			}

			gambler.NotifyTimer = !gambler.NotifyTimer

			err = saveGamblerData(gamblers, 0, "")
			handleError(err)

			var msgText string
			if gambler.NotifyTimer {
				msgText = fmt.Sprintf(
					"%s, вы включили уведомления о сбросе таймера гамбы.\n\nНапишите в ЛС боту любое сообщение, чтобы разрешить отправку уведомлений.",
					gambler.Username,
				)
			} else {
				msgText = fmt.Sprintf(
					"%s, вы отключили уведомления о сбросе таймера гамбы.",
					gambler.Username,
				)
			}
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
			msg.DisableNotification = true
			bot.Send(msg)
		}

		// Skip if the message is not a dice or is a forwarded message
		if update.Message.Dice == nil || update.Message.ForwardFrom != nil {
			continue
		}

		// Skip if the dice emoji is not 🎰
		if update.Message.Dice.Emoji != "🎰" {
			continue
		}

		// Handle gambles
		err = handleGamble(bot, update)
		handleError(err)
	}
}

func handleGamble(bot *tgbotapi.BotAPI, update tgbotapi.Update) (err error) {
	gamblers, err := loadGamblerData()
	if err != nil {
		return
	}
	gambler, ok := gamblers[update.Message.From.ID]
	if !ok {
		gambler = &Gambler{
			UserID:      update.Message.From.ID,
			Gambles:     0,
			GambleTime:  time.Now().Unix(),
			Username:    update.Message.From.UserName,
			Wins:        0,
			AllGambles:  0,
			NotifyTimer: false,
			Notified:    false,
		}
		gamblers[update.Message.From.ID] = gambler
	}

	timeSince := time.Since(time.Unix(gambler.GambleTime, 0))
	if timeSince.Minutes() < 60 {
		gambler.Gambles++
		gambler.Notified = false
	} else {
		gambler.Gambles = 1
		gambler.GambleTime = time.Now().Unix()
	}

	if gambler.Gambles > 3 {
		minutes := int(60 - timeSince.Minutes())
		seconds := 60 - (int(timeSince.Seconds()) % 60)

		msgText := fmt.Sprintf(
			"%s, лимит гамбы превышен!\nПравила гамбы: 3 крутки в час\n\nПопробуйте снова через %d минут %d секунд!\n",
			gambler.Username, minutes, seconds,
		)

		err := sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, msgText, 5, false)
		gambler.Gambles = 3
		return err
	} else {
		switch update.Message.Dice.Value {
		case 1, 22, 43, 64:
			gambler.Wins++
		}
		gambler.AllGambles++

		err := saveGamblerData(gamblers, update.Message.Dice.Value, update.Message.From.UserName)
		return err
	}
}
func getTopGamblers(gamblers map[int64]*Gambler, bot *tgbotapi.BotAPI, chatID int64) string {
	var topGamblers []*Gambler

	// Iterate over the gamblers map and filter out gamblers who are not in this chat.
	for _, gambler := range gamblers {
		chatMember, _ := bot.GetChatMember(tgbotapi.GetChatMemberConfig{
			ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
				ChatID: chatID,
				UserID: gambler.UserID,
			},
		})
		if chatMember.Status == "administrator" || chatMember.Status == "creator" || chatMember.Status == "member" {
			topGamblers = append(topGamblers, gambler)
		}
	}

	// Sort the top gamblers based on their win count and winrate.
	sort.Slice(topGamblers, func(i, j int) bool {
		if topGamblers[i].Wins == 0 && topGamblers[j].Wins == 0 {
			return topGamblers[i].AllGambles < topGamblers[j].AllGambles
		}
		winRateI := float64(topGamblers[i].Wins) / float64(topGamblers[i].AllGambles)
		winRateJ := float64(topGamblers[j].Wins) / float64(topGamblers[j].AllGambles)

		// If the win count for any of the gamblers is greater than the other, they are greater.
		// If the win count is equal, compare the win rate, gambler with higher winrate is greater.
		if topGamblers[i].Wins > topGamblers[j].Wins {
			return true
		} else if topGamblers[i].Wins == topGamblers[j].Wins {
			return winRateI > winRateJ
		}
		return false
	})

	var topGamblersText = "Правила гамбы: 3 крутки в час\n\n🎰 ТОП ГАМБЫ\n\n"
	for _, gambler := range topGamblers {
		topGamblersText += fmt.Sprintf("%s - %d побед - %d круток\n", gambler.Username, gambler.Wins, gambler.AllGambles)
	}

	return topGamblersText
}
func sendMessageAndDeleteAfterDelay(bot *tgbotapi.BotAPI, chatID int64, messageID int, text string, delay_time float64, isMarkdown bool) error {
	// Create the message to send
	var deleteSticker tgbotapi.DeleteMessageConfig
	doStickerExist := false
	message := tgbotapi.NewMessage(chatID, text)
	message.DisableNotification = true
	if isMarkdown {
		message.ParseMode = "Markdown"
	}

	// Delete the original message
	bot.Send(tgbotapi.NewDeleteMessage(chatID, messageID))

	if rand.IntN(5) == 4 && delay_time == 5 {
		doStickerExist = true
		stickerset, err := bot.GetStickerSet(tgbotapi.GetStickerSetConfig{Name: "ChoZaHui_nya_by_fStikBot"})
		if err != nil {
			return err
		}
		stickerMsg := tgbotapi.NewSticker(chatID, tgbotapi.FileID(stickerset.Stickers[0].FileID))
		stickerMsg.DisableNotification = true
		sentSticker, err := bot.Send(stickerMsg)
		if err != nil {
			return err
		}
		deleteSticker = tgbotapi.NewDeleteMessage(chatID, sentSticker.MessageID)
	}
	// Send the message and get the sent message
	sentMessage, err := bot.Send(message)
	if err != nil {
		return err
	}

	// Create a message to delete the sent message
	deleteMessage := tgbotapi.NewDeleteMessage(chatID, sentMessage.MessageID)

	// Start a goroutine to delete the sent message after the specified delay
	go func() {
		delay := time.Duration(delay_time) * time.Second
		time.Sleep(delay)
		if doStickerExist {
			bot.Send(deleteSticker)
		}
		bot.Send(deleteMessage)
	}()

	return nil
}
func notify(bot *tgbotapi.BotAPI, gambler *Gambler) (err error) {
	msg_text := fmt.Sprintf(
		"@%s, время гамбы!\n\nОтключить уведомления можно с помощью команды /notify",
		gambler.Username,
	)

	notification := tgbotapi.NewMessage(gambler.UserID, msg_text)
	_, err = bot.Send(notification)
	return err
}
func handleError(err error) {
	if err != nil {
		log.Println("Handled error!")
		log.Fatal(err)
	}
}
