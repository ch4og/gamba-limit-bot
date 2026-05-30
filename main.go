package main

import (
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"sort"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

var mu sync.Mutex

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
	godotenv.Load()

	// Create bot
	telegramToken := os.Getenv("TELEGRAM_API_TOKEN")
	bot, err := tgbotapi.NewBotAPI(telegramToken)
	handleError(err)

	botUsername := bot.Self.UserName
	log.Printf("Authorized on account %s", botUsername)
	botLink := fmt.Sprintf("https://t.me/%s", botUsername)

	err = initDB()
	handleError(err)

	go func() {
		for {
			time.Sleep(time.Second * 20)

			mu.Lock()
			gamblers, err := loadGamblerData()
			if err != nil {
				log.Printf("Failed to load gambler data: %v", err)
				mu.Unlock()
				continue
			}
			for _, gambler := range gamblers {
				if gambler.NotifyTimer {
					sinceGamble := time.Since(time.Unix(gambler.GambleTime, 0))
					if sinceGamble.Minutes() > 60 && !gambler.Notified {
						err = notify(bot, gambler)
						if err != nil {
							log.Printf("Can't send message to %s", gambler.Username)
						} else {
							gambler.Notified = true
							saveGamblerData(gambler, 0, "")
						}
					}
				}
			}
			mu.Unlock()
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

		if time.Since(update.Message.Time()) > time.Second*60 {
			continue
		}

		if update.Message.Text == "/top" || update.Message.Text == "/top@"+botUsername {
			gamblers, err := loadGamblerData()
			handleError(err)
			topText := getTopGamblers(gamblers, bot, update.Message.Chat.ID)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, topText)
			msg.ParseMode = "HTML"
			msg.DisableNotification = true
			bot.Send(msg)
		}

		if update.Message.Text == "/stats" || update.Message.Text == "/stats@"+botUsername {
			pullStats, err := loadPullStats()
			handleError(err)
			statsText := getDropStats(pullStats)
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, statsText)
			msg.ParseMode = "HTML"
			msg.DisableNotification = true
			bot.Send(msg)
		}

		if update.Message.Text == "/notify" || update.Message.Text == "/notify@"+botUsername {
			mu.Lock()
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
			}

			gambler.NotifyTimer = !gambler.NotifyTimer

			err = saveGamblerData(gambler, 0, "")
			mu.Unlock()
			handleError(err)

			var msgText string
			if gambler.NotifyTimer {
				msgText = fmt.Sprintf(
					"%s, вы включили уведомления о сбросе таймера гамбы.\n\nНапишите в [ЛС боту](%s) любое сообщение, чтобы разрешить отправку уведомлений.",
					gambler.Username,
					botLink,
				)
			} else {
				msgText = fmt.Sprintf(
					"%s, вы отключили уведомления о сбросе таймера гамбы.",
					gambler.Username,
				)
			}
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, msgText)
			msg.ParseMode = "Markdown"
			msg.DisableNotification = true
			bot.Send(msg)
		}

		// Skip if the message is not a dice or is a forwarded message
		if update.Message.Dice == nil || update.Message.ForwardFrom != nil {
			continue
		}

		// Skip if the dice emoji is not 🎰
		if update.Message.Dice.Emoji != "🎰" {
		        err := sendMessageAndDeleteAfterDelay(bot, update.Message.Chat.ID, update.Message.MessageID, "ЭТО НЕПРАВИЛЬНАЯ ГАМБА У НАС ТОКА СЛОТИКИ", 5, false)
		        handleError(err)
			continue
		}

		// Handle gambles
		err = handleGamble(bot, update)
		handleError(err)
	}
}

func handleGamble(bot *tgbotapi.BotAPI, update tgbotapi.Update) (err error) {
	mu.Lock()
	defer mu.Unlock()

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
		saveGamblerData(gambler, 0, "")
		return err
	}

	switch update.Message.Dice.Value {
	case 1, 22, 43, 64:
		gambler.Wins++
	}
	gambler.AllGambles++

	err = saveGamblerData(gambler, update.Message.Dice.Value, update.Message.From.UserName)
	return err
}
func getTopGamblers(gamblers map[int64]*Gambler, bot *tgbotapi.BotAPI, chatID int64) string {
	var topGamblers []*Gambler

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

	sort.Slice(topGamblers, func(i, j int) bool {
		if topGamblers[i].Wins == 0 && topGamblers[j].Wins == 0 {
			return topGamblers[i].AllGambles < topGamblers[j].AllGambles
		}
		winRateI := float64(topGamblers[i].Wins) / float64(topGamblers[i].AllGambles)
		winRateJ := float64(topGamblers[j].Wins) / float64(topGamblers[j].AllGambles)

		if topGamblers[i].Wins > topGamblers[j].Wins {
			return true
		} else if topGamblers[i].Wins == topGamblers[j].Wins {
			return winRateI > winRateJ
		}
		return false
	})

	sep := "━━━━━━━━━━━━━━━━━━━━━━"
	var text string
        text = "<code>"
	text += sep + "\n"
	text += "     🎰 <b>ТОП ГАМБЫ</b> 🎰\n"
	text += sep + "\n\n"

	maxUserLen := 0
	maxWinsLen := 0
	maxGamblesLen := 0
	for _, g := range topGamblers {
		if l := len(g.Username); l > maxUserLen {
			maxUserLen = l
		}
		if l := len(fmt.Sprintf("%d", g.Wins)); l > maxWinsLen {
			maxWinsLen = l
		}
		if l := len(fmt.Sprintf("%d", g.AllGambles)); l > maxGamblesLen {
			maxGamblesLen = l
		}
	}

	medals := []string{"🥇", "🥈", "🥉"}
	for i, gambler := range topGamblers {
		rank := i + 1
		var prefix string
		padding := maxUserLen
		if rank <= 3 {
			prefix = medals[rank-1] + " "
		} else {
			prefix = fmt.Sprintf("%2d. ", rank)
		}

		winRate := 0.0
		if gambler.AllGambles > 0 {
			winRate = float64(gambler.Wins) / float64(gambler.AllGambles) * 100
		}
		text += fmt.Sprintf("%s%-*s — %*d🏆 / %*d🎰 (%*.1f%%)\n",
			prefix,
			padding, gambler.Username,
			maxWinsLen, gambler.Wins,
			maxGamblesLen, gambler.AllGambles,
			5, winRate)
	}

        text += "</code>"
	return text
}

func getDropStats(pullStats map[string]int) string {
	sep := "━━━━━━━━━━━━━━━━━━━━━━"
	total := pullStats["bar"] + pullStats["grape"] + pullStats["lemon"] + pullStats["seven"]
	text := "<code>"
	text += sep + "\n"
	text += "📊 <b>СТАТИСТИКА</b>\n"
	text += sep + "\n"
	if total > 0 {
		text += fmt.Sprintf("🍾 BAR       %d (%5.1f%%)\n", pullStats["bar"], float64(pullStats["bar"])/float64(total)*100)
		text += fmt.Sprintf("🍇 Виноград  %d (%5.1f%%)\n", pullStats["grape"], float64(pullStats["grape"])/float64(total)*100)
		text += fmt.Sprintf("🍋 Лимон     %d (%5.1f%%)\n", pullStats["lemon"], float64(pullStats["lemon"])/float64(total)*100)
		text += fmt.Sprintf("7️⃣ Семерка   %d (%5.1f%%)\n", pullStats["seven"], float64(pullStats["seven"])/float64(total)*100)
	} else {
		text += fmt.Sprintf("🍾 BAR       %d\n", pullStats["bar"])
		text += fmt.Sprintf("🍇 Виноград  %d\n", pullStats["grape"])
		text += fmt.Sprintf("🍋 Лимон     %d\n", pullStats["lemon"])
		text += fmt.Sprintf("7️⃣ Семерка   %d\n", pullStats["seven"])
	}
	text += "</code>"
	return text
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
